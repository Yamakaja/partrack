package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Yamakaja/partrack/internal/paramparse"
)

// The --param grammar. Everything here compiles down to the server's
// param_{id}_min / param_{id}_max / param_{id}_values query parameters, which
// the parts endpoint fans out and intersects. Both numeric bounds are inclusive
// server-side, and value matching is exact string equality (or array overlap for
// enum_set), which is why `pt filters` prints the observed vocabulary.
//
//	resistance=10k        a band around 10 000 (±--param-tolerance, default 1%)
//	resistance=9k..11k    an explicit inclusive range
//	resistance=">=10k"    an open lower bound (> is accepted, also inclusive)
//	tolerance=1%          parsed by the same grammar the importer uses
//	mounting=SMD,THT      enum / text — matches any of the listed values
//
// Values go through internal/paramparse, so RKM (4k7, 0R22), SI prefixes,
// fractions (1/8W) and ppm all work exactly as they do on import.
type paramFilter struct {
	Def    *ParamDef
	Min    *float64
	Max    *float64
	Values []string
	Raw    string
}

// defaultParamTolerancePct is the half-width of the band applied to a bare
// value. 1% is tight enough to separate adjacent E96 values (~2.3% apart) while
// absorbing the float rounding that makes 0.1µF and 100nF differ in the last
// ulp.
const defaultParamTolerancePct = 1.0

// parseParamSpec parses one `slug=expr` argument against the parameter registry.
func parseParamSpec(spec string, rd *refData, tolerancePct float64) (*paramFilter, error) {
	slug, expr, ok := strings.Cut(spec, "=")
	if !ok {
		return nil, exitErrf(ExitUsage,
			"bad --param %q: expected slug=value (e.g. resistance=10k, tolerance=1%%, mounting=SMD)", spec)
	}
	slug = strings.TrimSpace(slug)
	expr = strings.TrimSpace(expr)
	if slug == "" || expr == "" {
		return nil, exitErrf(ExitUsage, "bad --param %q: both a slug and a value are required", spec)
	}
	if strings.EqualFold(slug, "package") {
		return nil, exitErrf(ExitUsage, "package is not a parameter — filter it with --package %s", expr)
	}

	def := rd.paramBySlug(slug)
	if def == nil {
		return nil, exitErrf(ExitNotFound,
			"unknown parameter %q — list a category's parameters with `pt filters <category>`", slug)
	}

	f := &paramFilter{Def: def, Raw: spec}

	// Text-shaped parameters can only be matched by value.
	if def.ValueType == "text" || def.ValueType == "enum" || def.ValueType == "enum_set" {
		for _, v := range strings.Split(expr, ",") {
			if v = strings.TrimSpace(v); v != "" {
				f.Values = append(f.Values, v)
			}
		}
		if len(f.Values) == 0 {
			return nil, exitErrf(ExitUsage, "bad --param %q: no values given", spec)
		}
		return f, nil
	}

	switch {
	case strings.Contains(expr, ".."):
		lo, hi, _ := strings.Cut(expr, "..")
		lo, hi = strings.TrimSpace(lo), strings.TrimSpace(hi)
		if lo == "" && hi == "" {
			return nil, exitErrf(ExitUsage, "bad --param %q: a range needs at least one bound", spec)
		}
		if lo != "" {
			v, err := parseParamNumber(lo, spec)
			if err != nil {
				return nil, err
			}
			f.Min = &v
		}
		if hi != "" {
			v, err := parseParamNumber(hi, spec)
			if err != nil {
				return nil, err
			}
			f.Max = &v
		}

	case strings.HasPrefix(expr, ">="), strings.HasPrefix(expr, ">"):
		v, err := parseParamNumber(strings.TrimLeft(expr, ">= "), spec)
		if err != nil {
			return nil, err
		}
		f.Min = &v

	case strings.HasPrefix(expr, "<="), strings.HasPrefix(expr, "<"):
		v, err := parseParamNumber(strings.TrimLeft(expr, "<= "), spec)
		if err != nil {
			return nil, err
		}
		f.Max = &v

	default:
		v, err := parseParamNumber(expr, spec)
		if err != nil {
			return nil, err
		}
		if tolerancePct <= 0 {
			lo, hi := v, v
			f.Min, f.Max = &lo, &hi
			break
		}
		band := absFloat(v) * tolerancePct / 100
		lo, hi := v-band, v+band
		f.Min, f.Max = &lo, &hi
	}

	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return nil, exitErrf(ExitUsage, "bad --param %q: lower bound exceeds upper bound", spec)
	}
	return f, nil
}

// parseParamNumber runs one side of a numeric expression through the shared
// value grammar. It always asks for "numeric" rather than the definition's own
// value type, since paramparse deliberately refuses to parse text and enum.
func parseParamNumber(text, spec string) (float64, error) {
	res := paramparse.Parse(text, "numeric")
	if res.Value == nil {
		return 0, exitErrf(ExitUsage, "bad --param %q: cannot read %q as a number", spec, text)
	}
	return *res.Value, nil
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// rangeCaveat returns a warning when this filter targets a range-typed
// parameter, or "" otherwise.
//
// A range parameter stores the span's lower bound in numeric_value and the upper
// bound in numeric_value_max ("-40°C ~ 125°C" → -40, 125). The server's range
// filter compares only numeric_value, so a bound here constrains the *start* of
// each part's span, not whether the span covers a point. Asking for
// `temperature_range=85` therefore matches parts whose minimum rating is 85 °C,
// which is almost never the question being asked — so say so rather than
// returning a confidently wrong result set.
func (f *paramFilter) rangeCaveat() string {
	if f.Def.ValueType != "range" || len(f.Values) > 0 {
		return ""
	}
	return fmt.Sprintf(
		"%s is a range parameter (%s): the filter compares the lower bound of each part's span, not whether the span covers your value",
		f.Def.Slug, f.Def.DisplayName)
}

// apply writes the filter into a query string.
func (f *paramFilter) apply(q url.Values) {
	key := "param_" + strconv.FormatInt(int64(f.Def.ID), 10)
	if f.Min != nil {
		q.Set(key+"_min", formatFloat(*f.Min))
	}
	if f.Max != nil {
		q.Set(key+"_max", formatFloat(*f.Max))
	}
	for _, v := range f.Values {
		q.Add(key+"_values", v)
	}
}

// describe renders the filter for the search footer, so the user can see what a
// bare value actually became.
func (f *paramFilter) describe() string {
	unit := ""
	if f.Def.UnitSymbol != nil {
		unit = *f.Def.UnitSymbol
	}
	switch {
	case len(f.Values) > 0:
		return fmt.Sprintf("%s = %s", f.Def.Slug, strings.Join(f.Values, "|"))
	case f.Min != nil && f.Max != nil && *f.Min == *f.Max:
		return fmt.Sprintf("%s = %s", f.Def.Slug, paramparse.FormatEngineering(*f.Min, unit))
	case f.Min != nil && f.Max != nil:
		return fmt.Sprintf("%s %s..%s", f.Def.Slug,
			paramparse.FormatEngineering(*f.Min, unit), paramparse.FormatEngineering(*f.Max, unit))
	case f.Min != nil:
		return fmt.Sprintf("%s >= %s", f.Def.Slug, paramparse.FormatEngineering(*f.Min, unit))
	case f.Max != nil:
		return fmt.Sprintf("%s <= %s", f.Def.Slug, paramparse.FormatEngineering(*f.Max, unit))
	}
	return f.Def.Slug
}

// formatFloat renders a bound for the query string without exponent notation
// surprises, since the server parses it with strconv.ParseFloat.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

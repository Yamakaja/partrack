package paramparse

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ParseResult holds the outcome of parsing one parameter value string.
// Value is nil when the text cannot be parsed as a number (non-numeric types,
// unparseable text). ValueMax is non-nil only for range parameters.
type ParseResult struct {
	Value    *float64
	ValueMax *float64
}

// numericRe matches an optional sign, digits (with optional decimal point),
// an optional SI prefix character, and an optional unit suffix.
// Groups: 1=sign+digits, 2=prefix, 3=unit-text (may be empty).
var numericRe = regexp.MustCompile(
	`^\s*([+-]?[0-9]+(?:\.[0-9]*)?(?:[eE][+-]?[0-9]+)?)\s*` +
		`([TGMkmKunpµ]?)` +
		`\s*([A-Za-zΩ°%/]*)\s*$`,
)

// toleranceRe matches tolerance strings like "±1%", "± 0.5 %", "+/-5%".
var toleranceRe = regexp.MustCompile(
	`^[±+\-/]*\s*([0-9]+(?:\.[0-9]*)?)\s*%\s*$`,
)

// ppmToleranceRe matches ppm tolerance strings like "±30ppm", "30 ppm".
// The captured value must be converted to percent (× 1e-4) before storing.
var ppmToleranceRe = regexp.MustCompile(
	`^[±+\-/]*\s*([0-9]+(?:\.[0-9]*)?)\s*ppm\s*$`,
)

// ppbToleranceRe matches ppb tolerance strings like "±500ppb", "500 ppb".
// The captured value must be converted to percent (× 1e-7) before storing.
var ppbToleranceRe = regexp.MustCompile(
	`^[±+\-/]*\s*([0-9]+(?:\.[0-9]*)?)\s*ppb\s*$`,
)

// rangeRe matches "X ~ Y" range patterns (after normalizeText substitution).
var rangeRe = regexp.MustCompile(
	`^(.+?)\s*~\s*(.+)$`,
)

// inchMmRe matches dual-unit patterns like `0.015" (0.37mm)` and captures
// the metric millimetre value so it can be used in place of the full string.
var inchMmRe = regexp.MustCompile(
	`^\s*[0-9]+(?:\.[0-9]*)?\"\s*\(([0-9]+(?:\.[0-9]*)?)mm\)\s*$`,
)

// trailingParenRe matches a trailing parenthetical annotation like " (x2 Channels)".
var trailingParenRe = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

// ppmRatioRe matches a magnitude followed by a ppm/ppb ratio unit, optionally
// with a denominator (e.g. `100 ppm/°C`, `50ppb/°C`). Group 2 captures m|b so the
// ratio can be normalised to the same percent-equivalent base the tolerance path
// uses — essential because ppm and ppb routinely appear on the *same* parameter
// (e.g. oscillator frequency stability: a coarse part in ppm, a precise one in
// ppb) and must stay numerically comparable. The leading "pp" must not be split as
// an SI prefix (the `p` in `ppm` is not pico). The denominator is treated as an
// opaque, per-parameter-consistent label. (Plain `100 ppm` tolerances are
// normalised earlier in Parse via ppmToleranceRe; this handles the compound and
// range forms that bypass it.)
var ppmRatioRe = regexp.MustCompile(
	`^([+-]?[0-9]+(?:\.[0-9]*)?(?:[eE][+-]?[0-9]+)?)\s*pp([mb])(?:\s*/.*)?$`,
)

// fractionRe matches a simple fraction with an optional SI prefix and unit,
// e.g. `1/8`, `1/16 W`, `1/4W`. Common for resistor power ratings.
// Groups: 1=numerator, 2=denominator, 3=prefix, 4=unit-text.
var fractionRe = regexp.MustCompile(
	`^([+-]?[0-9]+)\s*/\s*([0-9]+)\s*` +
		`([TGMkmKunpµ]?)` +
		`\s*([A-Za-zΩ°%]*)\s*$`,
)

// storageSizeRe matches storage size values with SI or IEC binary prefixes.
// Binary prefixes (Ki/Mi/Gi/Ti) must appear before single-char SI prefixes in the
// alternation so they are tried first. Groups: 1=number, 2=prefix, 3=unit.
var storageSizeRe = regexp.MustCompile(
	`^\s*([+-]?[0-9]+(?:\.[0-9]*)?(?:[eE][+-]?[0-9]+)?)\s*` +
		`(Ki|Mi|Gi|Ti|[kKMGT])?(bits?|Bits?|bytes?|Bytes?|B)\s*$`,
)

// Parse extracts numeric value(s) from distributor parameter text.
// valueType should be the partrack value_type ('numeric', 'range', 'text', 'enum').
// Returns a zero-value ParseResult (both nil) for non-numeric types or unparseable text.
func Parse(text string, valueType string) ParseResult {
	if valueType == "text" || valueType == "enum" {
		return ParseResult{}
	}

	text = strings.TrimSpace(text)
	if text == "" || text == "-" || text == "N/A" || text == "n/a" {
		return ParseResult{}
	}

	text = normalizeText(text)

	// Conditional / point spec: keep only the leading value; the bias conditions
	// after "@" (`40µA @ 60V`, `380mΩ @ 650mA, 4.5V`) are display-only context and
	// remain in the stored raw value. Stripping here also removes their commas before
	// the alternate-representation collapse below.
	if i := strings.IndexByte(text, '@'); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}

	// Dual-unit inch+mm: `0.015" (0.37mm)` → `0.37mm`
	if m := inchMmRe.FindStringSubmatch(text); m != nil {
		text = m[1] + "mm"
	} else {
		// Strip trailing parenthetical annotations: `800µA (x2 Channels)` → `800µA`
		text = trailingParenRe.ReplaceAllString(text, "")
	}

	// Tolerance: "±1%" → 1.0
	if m := toleranceRe.FindStringSubmatch(text); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			return ParseResult{Value: &v}
		}
	}

	// PPM tolerance: "±30ppm" → 0.003 (converted to %-equivalent: ppm × 1e-4)
	if m := ppmToleranceRe.FindStringSubmatch(text); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			v *= 1e-4
			return ParseResult{Value: &v}
		}
	}

	// PPB tolerance: "±500ppb" → 5e-5 (converted to %-equivalent: ppb × 1e-7)
	if m := ppbToleranceRe.FindStringSubmatch(text); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			v *= 1e-7
			return ParseResult{Value: &v}
		}
	}

	// Range: "X ~ Y"
	if m := rangeRe.FindStringSubmatch(text); m != nil {
		lo := parseScalar(m[1])
		hi := parseScalar(m[2])
		if lo != nil && hi != nil {
			return ParseResult{Value: lo, ValueMax: hi}
		}
		if lo != nil {
			return ParseResult{Value: lo}
		}
	}

	// Single scalar value
	v := parseScalar(text)
	if v != nil {
		return ParseResult{Value: v}
	}

	// Alternate representations: some distributors return the same value twice in
	// different forms ("0.063W, 1/16W"). Collapse to one when the comma-separated
	// parts agree; genuine tuples (differing values, e.g. "1.8V, 4.5V") are left
	// unparsed for later decomposition.
	if v := collapseEquivalent(text); v != nil {
		return ParseResult{Value: v}
	}

	return ParseResult{}
}

// collapseEquivalent parses comma-separated alternate representations of one value
// and returns it when all parts agree (within 2%, to tolerate a rounded decimal
// vs an exact fraction). Returns nil if there is no comma, any part is unparseable,
// or the parts represent distinct values (a tuple, not an alternate representation).
func collapseEquivalent(text string) *float64 {
	parts := strings.Split(text, ",")
	if len(parts) < 2 {
		return nil
	}
	var first *float64
	for _, p := range parts {
		v := parseScalar(strings.TrimSpace(p))
		if v == nil {
			return nil
		}
		if first == nil {
			first = v
		} else if !approxEqual(*first, *v) {
			return nil
		}
	}
	return first
}

// approxEqual reports whether a and b are equal within 2% relative tolerance.
func approxEqual(a, b float64) bool {
	if a == b {
		return true
	}
	m := math.Max(math.Abs(a), math.Abs(b))
	if m == 0 {
		return true
	}
	return math.Abs(a-b)/m <= 0.02
}

// ParseEnumSet splits a multi-valued enum string ("CANbus, EBI/EMI, Ethernet")
// into its distinct member values, trimming whitespace and dropping empties.
// Order is preserved and duplicates are removed. Returns nil when there are no
// members so callers store SQL NULL rather than an empty array.
func ParseEnumSet(text string) []string {
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseScalar attempts to parse a single numeric value (with optional SI prefix)
// from s. Returns nil if the string is not parseable.
func parseScalar(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}

	// Strip leading ± or + for tolerances / one-sided ranges
	s = strings.TrimLeft(s, "±+ ")

	// Fast path for purely numeric strings (no unit)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return &v
	}

	// Fraction: "1/8", "1/16 W", "3/8W" → decimal (resistor power ratings etc.)
	if m := fractionRe.FindStringSubmatch(s); m != nil {
		num, errN := strconv.ParseFloat(m[1], 64)
		den, errD := strconv.ParseFloat(m[2], 64)
		if errN == nil && errD == nil && den != 0 {
			v := (num / den) * prefixMultiplier(m[3])
			return &v
		}
	}

	// ppm/ppb ratio unit: "100 ppm/°C" → 0.01, "50 ppb/°C" → 5e-6. Normalised to the
	// percent-equivalent base (1 ppm = 1e-4 %, 1 ppb = 1e-7 %) so ppm and ppb on one
	// parameter stay comparable. Checked before numericRe, which would otherwise
	// greedily read the first "p" of "ppm" as pico.
	if m := ppmRatioRe.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			if m[2] == "b" {
				v *= 1e-7
			} else {
				v *= 1e-4
			}
			return &v
		}
	}

	// Storage size: "9 Mbit", "1 KiB", "256 GiB" → value in bits
	if m := storageSizeRe.FindStringSubmatch(s); m != nil {
		base, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			v := base * storageMultiplier(m[2], m[3])
			return &v
		}
	}

	m := numericRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	numStr := m[1]
	prefix := m[2]

	// Sanity-check: the "unit" part should not look like digits.
	// If the entire string is digits+letters with no recognisable SI prefix,
	// check if it's a package code like "0402" — no prefix means prefix="".
	if prefix == "" && m[3] != "" {
		// There is a unit suffix but no SI prefix.
		// Allow known alphanumeric unit suffixes (Ohm, Hz, V, A, W, F, H, etc.)
		// Reject if the "unit" part is purely digits (e.g. "0402").
		if isAllDigits(m[3]) {
			return nil
		}
	}

	base, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return nil
	}

	result := base * prefixMultiplier(prefix)
	return &result
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

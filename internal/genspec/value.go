package genspec

import (
	"regexp"
	"strings"

	"github.com/Yamakaja/partrack/internal/paramparse"
)

// Param is one extracted parameter constraint. ValueSI is nil for enum/text
// params (e.g. dielectric, colour) and for unparseable numerics.
type Param struct {
	Slug    string   `json:"slug"`
	Raw     string   `json:"raw"`
	ValueSI *float64 `json:"value_si"`
}

// dielectrics is the recognised set of capacitor dielectric codes (uppercased).
var dielectrics = map[string]bool{
	"C0G": true, "NP0": true, "X7R": true, "X5R": true, "X8R": true,
	"X6S": true, "X7S": true, "X7T": true, "Y5V": true, "Z5U": true,
	"Y5U": true, "U2J": true, "C0H": true,
}

// colors is the recognised set of LED colour names (lowercased).
var colors = map[string]bool{
	"red": true, "green": true, "blue": true, "white": true, "yellow": true,
	"orange": true, "amber": true, "uv": true, "ir": true, "rgb": true,
	"warmwhite": true, "coolwhite": true, "pink": true, "purple": true,
}

var (
	pureNumberRe   = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)
	toleranceTokRe = regexp.MustCompile(`(?i)^[±+\-]?\d+(\.\d+)?\s*(%|ppm|ppb)$`)
	voltageTokRe   = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s*[kKmµun]?V$`)
	currentTokRe   = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s*[kKmµun]?A$`)
	powerTokRe     = regexp.MustCompile(`(?i)^(\d+/\d+|\d+(\.\d+)?)\s*[mµu]?W$`)
)

// ParseValue parses a value field given a class profile. It returns the extracted
// parameters, an optional package code found leaking in the value field,
// mpnLike=true when the value reads as an identifier rather than a parametric
// magnitude (→ route to identity resolution), and the residue: the tokens it
// could not attribute to a parameter or package. Residue is the escalation
// signal — a clean parametric value leaves none (see Interpret).
func ParseValue(p Profile, value string) (params []Param, pkg string, mpnLike bool, residue []string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, "", false, nil
	}

	tokens := mergeNumberUnit(tokenize(value))

	var (
		foundPrimary bool
		foundEnum    bool
	)

	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		// 1. Secondary recognizers, gated by the class's plausible secondaries.
		if slug, prm, ok := recognizeSecondary(p, tok); ok {
			params = append(params, prm)
			if slug == "dielectric" || slug == "color" {
				foundEnum = true
			}
			continue
		}
		// 2. A bare, leading-zero imperial code is a package leaking into the value
		//    (e.g. "10k 0402 1/16W"); unambiguous because a resistance is written
		//    "402", never "0402".
		if pkg == "" && strings.HasPrefix(tok, "0") && imperialSizes[tok] {
			pkg = tok
			continue
		}
		// 3. The primary magnitude (only if the class has one and we have not got it).
		if p.ValueParam != "" && !foundPrimary {
			if r := paramparse.Parse(tok, "numeric"); r.Value != nil {
				v := *r.Value
				params = append(params, Param{Slug: p.ValueParam, Raw: tok, ValueSI: &v})
				foundPrimary = true
				continue
			}
		}
		// 4. Unattributed. For a parametric class this is residue (something we
		//    could not place → escalate). For an identity class the value IS an
		//    identifier, so leftover tokens are expected, not residue.
		if p.ValueParam != "" {
			residue = append(residue, tok)
		}
	}

	switch {
	case p.ValueParam != "":
		// Parametric class: identifier-like content with no magnitude → MPN.
		mpnLike = !foundPrimary && len(residue) > 0
	default:
		// Identity-ish class (no magnitude): parametric only if an enum secondary
		// (e.g. an LED colour) accounted for the value; otherwise it is an MPN.
		mpnLike = !foundEnum
	}
	return params, pkg, mpnLike, residue
}

// recognizeSecondary tries the global secondary recognizers, returning a Param
// only when the matched slug is plausible for the class.
func recognizeSecondary(p Profile, tok string) (slug string, prm Param, ok bool) {
	up := strings.ToUpper(tok)
	low := strings.ToLower(tok)
	switch {
	case p.HasSecondary("dielectric") && dielectrics[up]:
		return "dielectric", Param{Slug: "dielectric", Raw: tok}, true
	case p.HasSecondary("color") && colors[low]:
		return "color", Param{Slug: "color", Raw: tok}, true
	case p.HasSecondary("tolerance") && toleranceTokRe.MatchString(tok):
		return "tolerance", numericParam("tolerance", tok), true
	case p.HasSecondary("voltage_rating") && voltageTokRe.MatchString(tok):
		return "voltage_rating", numericParam("voltage_rating", tok), true
	case p.HasSecondary("current_rating") && currentTokRe.MatchString(tok):
		return "current_rating", numericParam("current_rating", tok), true
	case p.HasSecondary("power_rating") && powerTokRe.MatchString(tok):
		return "power_rating", numericParam("power_rating", tok), true
	}
	return "", Param{}, false
}

func numericParam(slug, tok string) Param {
	prm := Param{Slug: slug, Raw: tok}
	if r := paramparse.Parse(tok, "numeric"); r.Value != nil {
		v := *r.Value
		prm.ValueSI = &v
	}
	return prm
}

// tokenize splits a value field on commas and whitespace.
func tokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

// mergeNumberUnit merges a lone numeric token with a following unit token, so
// "10 nF X7R" → ["10nF", "X7R"] while "100µF 16V 20%" is left untouched.
func mergeNumberUnit(toks []string) []string {
	out := make([]string, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		if pureNumberRe.MatchString(toks[i]) && i+1 < len(toks) && startsWithUnit(toks[i+1]) {
			out = append(out, toks[i]+toks[i+1])
			i++
			continue
		}
		out = append(out, toks[i])
	}
	return out
}

// startsWithUnit reports whether a token begins with a unit-ish character (a
// letter or Ω/µ) rather than a digit — i.e. it is a unit for a preceding number.
func startsWithUnit(tok string) bool {
	if tok == "" {
		return false
	}
	r := []rune(tok)[0]
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == 'Ω' || r == 'µ' || r == 'μ'
}

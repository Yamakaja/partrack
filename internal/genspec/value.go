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
	fractionTokRe  = regexp.MustCompile(`^\d+/\d+$`)
	toleranceTokRe = regexp.MustCompile(`(?i)^[±+\-]?\d+(\.\d+)?\s*(%|ppm|ppb)$`)
	voltageTokRe   = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s*[kKmµun]?V$`)
	currentTokRe   = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s*[kKmµun]?A$`)
	powerTokRe     = regexp.MustCompile(`(?i)^(\d+/\d+|\d+(\.\d+)?)\s*[mµu]?W$`)
	// fieldSlashRe matches a slash used as a field separator — one immediately
	// preceded by a unit or SI-prefix character (a letter, Ω, µ, or %), as in
	// "10uF/25V", "100n/50V" or "10k/1%". A power-rating fraction's slash is always
	// flanked by bare digits ("1/2W"), so it is preceded by a digit, left untouched,
	// and still parses via the fraction path. See splitFieldSlashes.
	fieldSlashRe = regexp.MustCompile(`([A-Za-zΩµμ%])\s*/\s*`)
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

	tokens := mergeNumberUnit(tokenize(splitFieldSlashes(value)))

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

// genericValueTokens are device-type and descriptor words that EDA tools emit as a
// symbol's default "value" when no real part is assigned (e.g. KiCad's "LED",
// "FerriteBead_Small", "Conn_01x04_Pin"). A value built only from these (plus
// pin-count/pitch descriptors) names a commodity, not a manufacturer part — so it
// must not be read as an MPN. Lowercased.
var genericValueTokens = map[string]bool{
	// device types
	"led": true, "ferrite": true, "ferritebead": true, "bead": true,
	"resistor": true, "res": true, "capacitor": true, "cap": true,
	"inductor": true, "ind": true, "coil": true, "choke": true,
	"crystal": true, "xtal": true, "oscillator": true, "osc": true, "resonator": true,
	"diode": true, "transistor": true, "mosfet": true, "fet": true, "bjt": true,
	"fuse": true, "varistor": true, "mov": true, "transformer": true, "relay": true,
	"conn": true, "connector": true, "header": true, "pinheader": true, "pin": true,
	"socket": true, "jack": true, "plug": true, "terminal": true, "screw": true,
	"jumper": true, "solderjumper": true, "switch": true, "button": true,
	"testpoint": true, "test": true, "point": true, "mountinghole": true,
	"mounting": true, "hole": true, "antenna": true, "battery": true, "speaker": true,
	"buzzer": true, "potentiometer": true, "pot": true, "trimmer": true,
	// descriptors
	"small": true, "medium": true, "large": true, "tiny": true, "std": true,
	"generic": true, "value": true, "device": true, "part": true,
	"vertical": true, "horizontal": true, "male": true, "female": true,
	"top": true, "bottom": true, "left": true, "right": true, "angled": true,
	"smd": true, "tht": true, "smt": true, "thru": true, "throughhole": true,
}

var (
	placeholderSplitRe = regexp.MustCompile(`[\s_\-/]+`)
	pinCountPitchRe    = regexp.MustCompile(`(?i)^(p?\d+(\.\d+)?mm|\d+x\d+|\d+p(in)?|\d+pos|x\d+)$`)
)

// looksLikeGenericPlaceholder reports whether a value field is a generic device-type
// / schematic-symbol name (e.g. "LED", "FerriteBead_Small", "Conn_01x04_Pin")
// rather than a real identifier. True only when EVERY token is a known generic word
// or a pin-count/pitch descriptor and at least one is a device word — so real MPNs
// (e.g. "LM358", "STM32F407VGT6") are never misread.
func looksLikeGenericPlaceholder(value string) bool {
	toks := placeholderSplitRe.Split(strings.TrimSpace(value), -1)
	sawDevice := false
	for _, t := range toks {
		if t == "" {
			continue
		}
		low := strings.ToLower(t)
		switch {
		case genericValueTokens[low]:
			sawDevice = true
		case pinCountPitchRe.MatchString(low) || pureNumberRe.MatchString(low):
			// allowed descriptor, but not itself a device word
		default:
			return false // a token we can't account for → treat as a real identifier
		}
	}
	return sawDevice
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

// splitFieldSlashes rewrites field-separator slashes to spaces so each side
// tokenizes independently (e.g. "10uF/25V" → "10uF 25V"), without disturbing an
// int/int power fraction (e.g. "1/2W"), whose slash is flanked by bare digits.
func splitFieldSlashes(s string) string {
	return fieldSlashRe.ReplaceAllString(s, "$1 ")
}

// tokenize splits a value field on commas and whitespace.
func tokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

// mergeNumberUnit merges a lone numeric token with a following unit token, so
// "10 nF X7R" → ["10nF", "X7R"] while "100µF 16V 20%" is left untouched. A bare
// int/int fraction merges the same way, so a spaced power rating like
// "0.1 1/2 W" rejoins into ["0.1", "1/2W"].
func mergeNumberUnit(toks []string) []string {
	out := make([]string, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		if (pureNumberRe.MatchString(toks[i]) || fractionTokRe.MatchString(toks[i])) &&
			i+1 < len(toks) && startsWithUnit(toks[i+1]) {
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

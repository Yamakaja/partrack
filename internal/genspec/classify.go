package genspec

import (
	"regexp"
	"slices"
	"strings"
)

// Profile is the parsing profile for a component class — the in-code default for
// what will eventually be sourced from categories.value_param (the primary
// magnitude) and categories.primary_params (the plausible secondaries).
type Profile struct {
	Class        string   // class key, e.g. "R"
	CategorySlug string   // partrack category slug, e.g. "resistors"
	ValueParam   string   // primary parameter slug; "" => no numeric magnitude (identity-ish)
	UnitSymbol   string   // display unit for the primary value, e.g. "Ω"
	Secondaries  []string // plausible secondary parameter slugs (gates global recognizers)
}

// HasSecondary reports whether slug is a plausible secondary for this class.
func (p Profile) HasSecondary(slug string) bool {
	return slices.Contains(p.Secondaries, slug)
}

var refdesPrefixRe = regexp.MustCompile(`^([A-Za-z]+)`)

// ignoreClasses are virtual / non-procurable designators.
var ignoreClasses = map[string]bool{
	"TP": true, "FID": true, "MH": true, "H": true,
}

// prefixToClass maps a reference-designator alpha prefix to a class key. The full
// alpha prefix is the key, so multi-letter prefixes (FB, LED, RV) are matched
// ahead of their single-letter cousins (F, L, R) automatically.
var prefixToClass = map[string]string{
	"R": "R", "C": "C", "L": "L", "FB": "FB",
	"D": "D", "LED": "LED", "Q": "Q",
	"U": "U", "IC": "U", "Y": "Y", "X": "Y", "XTAL": "Y",
	"J": "J", "P": "J", "CN": "J", "CON": "J",
	"SW": "SW", "S": "SW", "JP": "JP",
	"RV": "RV", "VR": "RV", "F": "F", "T": "T",
	"TP": "TP", "FID": "FID", "MH": "MH", "H": "H",
}

// profiles is the in-code class-profile registry. Classes with a ValueParam are
// parametric (their value field is a magnitude); classes with an empty ValueParam
// and no enum secondary are identity (the value is an MPN).
var profiles = map[string]Profile{
	"R":   {Class: "R", CategorySlug: "resistors", ValueParam: "resistance", UnitSymbol: "Ω", Secondaries: []string{"tolerance", "power_rating"}},
	"C":   {Class: "C", CategorySlug: "capacitors", ValueParam: "capacitance", UnitSymbol: "F", Secondaries: []string{"voltage_rating", "dielectric", "tolerance"}},
	"L":   {Class: "L", CategorySlug: "inductors-coils-chokes", ValueParam: "inductance", UnitSymbol: "H", Secondaries: []string{"tolerance", "current_rating"}},
	"FB":  {Class: "FB", CategorySlug: "filters", ValueParam: "resistance", UnitSymbol: "Ω", Secondaries: []string{"current_rating"}},
	"Y":   {Class: "Y", CategorySlug: "crystals-oscillators-resonators", ValueParam: "frequency", UnitSymbol: "Hz", Secondaries: []string{"tolerance"}},
	"F":   {Class: "F", CategorySlug: "circuit-protection", ValueParam: "current_rating", UnitSymbol: "A", Secondaries: []string{"voltage_rating"}},
	"LED": {Class: "LED", CategorySlug: "optoelectronics", ValueParam: "", Secondaries: []string{"color"}},
	"D":   {Class: "D", CategorySlug: "discrete-semiconductor-products"},
	"Q":   {Class: "Q", CategorySlug: "discrete-semiconductor-products"},
	"U":   {Class: "U", CategorySlug: "integrated-circuits-ics"},
	"J":   {Class: "J", CategorySlug: "connectors-interconnects"},
	"SW":  {Class: "SW", CategorySlug: "switches"},
	"RV":  {Class: "RV", CategorySlug: "circuit-protection", ValueParam: "voltage_rating", UnitSymbol: "V"},
	"T":   {Class: "T", CategorySlug: "magnetics-transformer-inductor-components"},
	"JP":  {Class: "JP"},
}

// ClassifyRefdes returns the class key for a reference designator's alpha prefix.
func ClassifyRefdes(refdes string) (string, bool) {
	m := refdesPrefixRe.FindString(strings.TrimSpace(refdes))
	if m == "" {
		return "", false
	}
	if c, ok := prefixToClass[strings.ToUpper(m)]; ok {
		return c, true
	}
	return "", false
}

// IsIgnoredClass reports whether a class key denotes a virtual/non-procurable part.
func IsIgnoredClass(class string) bool { return ignoreClasses[class] }

// ProfileForClass returns the parsing profile for a class key.
func ProfileForClass(class string) (Profile, bool) {
	p, ok := profiles[class]
	return p, ok
}

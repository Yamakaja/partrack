// Package genericpart synthesizes a human-readable identity (MPN + description) for
// a generic/parametric part from its category configuration and resolved parameters.
// It is the single source shared by the create-generic handler and the startup
// backfill, so newly created and pre-existing generic parts read identically.
package genericpart

import (
	"sort"
	"strings"

	"github.com/Yamakaja/partrack/internal/paramparse"
)

// Param is one resolved parameter for identity synthesis. Numeric is set for
// magnitude params (with UnitSymbol from the parameter definition); Value carries the
// raw text for enum/text params.
type Param struct {
	Slug       string
	Numeric    *float64
	UnitSymbol string
	Value      string
}

// Identity is the synthesized MPN and description for a generic part.
type Identity struct {
	MPN         string
	Description string
}

// Synthesize builds an MPN and description from a category's generic configuration
// (code + value_param + ordered primary_params + display name), its canonical
// package, and the part's parameters. ok is false when the category has no
// generic_code, i.e. it does not support generic-part synthesis.
//
// MPN  : GEN-<code>-<value>-<package>[-<secondary>…]  e.g. GEN-RES-10K-0402-1%
// Desc : <value> [<secondary>…] <package> <Noun>      e.g. 10 kΩ ±1% 0402 Resistor
//
// Every parameter is encoded into the MPN (declared secondaries first, then any
// extras sorted by slug) so the MPN is a 1:1 function of the dedup signature.
func Synthesize(code, valueParam string, paramOrder []string, displayName, canonicalPkg string, params []Param) (Identity, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return Identity{}, false
	}

	bySlug := make(map[string]Param, len(params))
	for _, p := range params {
		bySlug[p.Slug] = p
	}

	mpnParts := []string{"GEN", code}
	var descParts []string

	// Primary magnitude.
	if vp, ok := bySlug[valueParam]; ok && vp.Numeric != nil {
		mpnParts = append(mpnParts, paramparse.FormatRKM(*vp.Numeric, vp.UnitSymbol))
		descParts = append(descParts, paramparse.FormatEngineering(*vp.Numeric, vp.UnitSymbol))
	}

	// Package.
	if canonicalPkg != "" {
		mpnParts = append(mpnParts, mpnToken(canonicalPkg))
	}

	// Secondaries: the category's declared order first, then any remaining params
	// (sorted) so the MPN encodes the full parameter signature.
	emitted := map[string]bool{valueParam: true, "package": true}
	var descSecondary []string
	emit := func(slug string) {
		if emitted[slug] {
			return
		}
		p, ok := bySlug[slug]
		if !ok {
			return
		}
		emitted[slug] = true
		mpnParts = append(mpnParts, secondaryMPN(p))
		descSecondary = append(descSecondary, secondaryDesc(p))
	}
	for _, slug := range paramOrder {
		emit(slug)
	}
	var extra []string
	for _, p := range params {
		if !emitted[p.Slug] {
			extra = append(extra, p.Slug)
		}
	}
	sort.Strings(extra)
	for _, slug := range extra {
		emit(slug)
	}

	// Description: value, secondaries, package, then the singular category noun.
	descParts = append(descParts, descSecondary...)
	if canonicalPkg != "" {
		descParts = append(descParts, canonicalPkg)
	}
	if noun := singularNoun(displayName); noun != "" {
		descParts = append(descParts, noun)
	}

	return Identity{
		MPN:         strings.Join(mpnParts, "-"),
		Description: strings.TrimSpace(strings.Join(descParts, " ")),
	}, true
}

func secondaryMPN(p Param) string {
	if p.Numeric != nil {
		return paramparse.FormatRKM(*p.Numeric, p.UnitSymbol)
	}
	return mpnToken(p.Value)
}

func secondaryDesc(p Param) string {
	if p.Numeric != nil {
		if p.UnitSymbol == "%" {
			return "±" + paramparse.FormatRKM(*p.Numeric, "%") // "±1%"
		}
		return paramparse.FormatEngineering(*p.Numeric, p.UnitSymbol)
	}
	return p.Value
}

// mpnToken keeps only alphanumerics (uppercased) so a token never introduces a stray
// "-" segment or whitespace into the dash-joined MPN.
func mpnToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// singularNoun turns a category display name into a singular noun for descriptions:
// the first comma-separated segment with a trailing plural "s" stripped, e.g.
// "Resistors" → "Resistor", "Inductors, Coils, Chokes" → "Inductor".
func singularNoun(displayName string) string {
	s := displayName
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ss"):
		return s
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	}
	return s
}

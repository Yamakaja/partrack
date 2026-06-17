package genspec

import (
	"regexp"
	"slices"
	"strings"
)

// Route classifies how a BOM line should be resolved downstream.
type Route string

const (
	RouteIdentity   Route = "identity"   // MPN/SKU or value-is-an-MPN → identity resolution
	RouteParametric Route = "parametric" // footprint + value → parametric match / generic part
	RouteIgnore     Route = "ignore"     // virtual / non-procurable
	RouteUnknown    Route = "unknown"    // nothing reliably extractable → needs review
)

// Input is the raw, role-mapped BOM line a parser sees. Any field may be empty.
type Input struct {
	Refdes    string
	Footprint string
	Value     string
	MPN       string // identity hint
	SKU       string // identity hint
}

// Interpretation is the structured reading of one BOM line (see doc/bom-import.md
// §5.6). It is produced by the parser and reviewed by the user before satisfaction.
type Interpretation struct {
	Route      Route    `json:"route"`
	Class      string   `json:"class"`             // class key, e.g. "R"
	Category   string   `json:"category"`          // partrack category slug, e.g. "resistors"
	Package    string   `json:"package"`           // canonical package; "" if none
	Mounting   string   `json:"mounting"`          // "smd" | "tht" | ""
	Params     []Param  `json:"params"`            // extracted constraints
	Flags      []string `json:"flags"`             // anything needing attention
	Confidence float64  `json:"confidence"`        // 0..1
	Residue    []string `json:"residue,omitempty"` // value tokens we could not attribute
	Escalate   bool     `json:"escalate"`          // deterministic parse is uncertain → LLM fallback eligible
}

// flagClassFootprintConflict marks a refdes/footprint class disagreement.
const flagClassFootprintConflict = "class_footprint_conflict"

var ignoreValueRe = regexp.MustCompile(`(?i)^(dnp|dni|dnf|nc|nf|do[ _]?not[ _]?(populate|place|fit)|not[ _]?fitted|logo|~)$`)

// Interpret parses a BOM line into a structured Interpretation.
func Interpret(in Input) Interpretation {
	it := Interpretation{Confidence: 1.0}

	refClass, hasRefClass := ClassifyRefdes(in.Refdes)
	pkg, mounting, fpClass, _ := CanonicalPackage(in.Footprint)
	it.Package, it.Mounting = pkg, mounting

	// Reconcile class from the reference designator and the footprint token.
	class := reconcileClass(refClass, hasRefClass, fpClass, &it)
	it.Class = class
	if p, ok := ProfileForClass(class); ok {
		it.Category = p.CategorySlug
	}

	// Virtual / non-procurable: ignored designator class or an explicit DNP marker.
	if IsIgnoredClass(class) || ignoreValueRe.MatchString(strings.TrimSpace(in.Value)) {
		it.Route = RouteIgnore
		return it
	}

	// Identity hints win outright.
	if strings.TrimSpace(in.MPN) != "" || strings.TrimSpace(in.SKU) != "" {
		it.Route = RouteIdentity
		return it
	}

	conflict := slices.Contains(it.Flags, flagClassFootprintConflict)

	value := strings.TrimSpace(in.Value)
	if class == "" {
		// No class to supply a unit: a bare value is unsafe to interpret. A value
		// we could not even classify is worth an LLM look (the full row may place it).
		it.Route = RouteUnknown
		it.Confidence = 0.2
		it.Escalate = value != ""
		return it
	}

	prof, _ := ProfileForClass(class)

	if value == "" {
		// Known class, nothing to resolve automatically. Nothing to escalate either
		// (no content) — but a class conflict is still worth resolving.
		it.Route = RouteUnknown
		it.Confidence = 0.3
		it.Escalate = conflict
		return it
	}

	params, valuePkg, mpnLike, residue := ParseValue(prof, value)
	it.Params = params
	if it.Package == "" && valuePkg != "" {
		it.Package = valuePkg
		if it.Mounting == "" {
			it.Mounting = "smd"
		}
	}

	switch {
	case mpnLike:
		it.Route = RouteIdentity
		it.Confidence = 0.8
	case len(params) > 0:
		it.Route = RouteParametric
	default:
		it.Route = RouteUnknown
		it.Confidence = 0.3
	}

	// Escalation: a parametric class is "clean" only when it yielded its primary
	// magnitude with no leftover tokens. Anything else — residue, a missing
	// magnitude (e.g. "9k10%", "5V/10uF"), or a class conflict — is fair game for
	// the LLM. Identity classes carry an MPN in the value, so leftover tokens there
	// are expected, not residue.
	if prof.ValueParam != "" {
		it.Residue = residue
		hasPrimary := false
		for _, p := range params {
			if p.Slug == prof.ValueParam {
				hasPrimary = true
				break
			}
		}
		it.Escalate = conflict || !hasPrimary || len(residue) > 0
	} else {
		it.Escalate = conflict
	}
	return it
}

// reconcileClass merges the reference-designator class and the footprint class
// token, flagging genuine conflicts. The refdes is authoritative, except that a
// footprint LED_ token refines a generic D (diode) designator to an LED.
func reconcileClass(refClass string, hasRef bool, fpClass string, it *Interpretation) string {
	switch {
	case !hasRef && fpClass == "":
		return ""
	case !hasRef:
		return fpClass
	case fpClass == "" || fpClass == refClass:
		return refClass
	case refClass == "D" && fpClass == "LED":
		return "LED"
	default:
		it.Flags = append(it.Flags, flagClassFootprintConflict)
		it.Confidence = 0.6
		return refClass
	}
}

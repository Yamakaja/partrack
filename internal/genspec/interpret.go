package genspec

import (
	"encoding/json"
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
	Mounting   string   `json:"mounting"`          // "smd" | "tht" | ""
	Params     []Param  `json:"params"`            // extracted constraints, incl. the "package" param
	Flags      []string `json:"flags"`             // anything needing attention
	Confidence float64  `json:"confidence"`        // 0..1
	Residue    []string `json:"residue,omitempty"` // value tokens we could not attribute
	Escalate   bool     `json:"escalate"`          // deterministic parse is uncertain → LLM fallback eligible

	// Identity fields are populated only by the LLM specific-part fallback (the
	// deterministic parser never sets them). They are overlaid onto the BOM line's
	// own columns at import — when those are empty — so the existing SKU/MPN
	// resolution can satisfy the line. See handler.escalateInterpretation.
	Mpn           string `json:"mpn,omitempty"`
	Manufacturer  string `json:"manufacturer,omitempty"`
	Sku           string `json:"sku,omitempty"`
	DistributorID string `json:"distributor_id,omitempty"`
}

// PackageSlug is the parameter slug under which a part's package/case is stored.
// Package is modelled as an ordinary parameter (it is a parameter_definition and
// appears in categories' primary_params); canonicalisation happens at the write
// boundary (genspec.NormalizePackage), never in the interpretation itself.
const PackageSlug = "package"

// UnmarshalJSON decodes an Interpretation, folding a legacy top-level "package"
// field (emitted before package became a parameter) into the params. This keeps
// older persisted interpretations and remembered corrections matchable without a
// data migration — every reader sees package the new way, via PackageValue().
func (it *Interpretation) UnmarshalJSON(data []byte) error {
	type alias Interpretation // no methods → no recursion
	aux := struct {
		*alias
		LegacyPackage string `json:"package"`
	}{alias: (*alias)(it)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.LegacyPackage != "" && it.PackageValue() == "" {
		it.SetPackage(aux.LegacyPackage)
	}
	return nil
}

// PackageValue returns the raw package/case value from the params, or "".
func (it Interpretation) PackageValue() string {
	for _, p := range it.Params {
		if p.Slug == PackageSlug {
			return p.Raw
		}
	}
	return ""
}

// SetPackage sets, replaces, or (when v is empty) removes the package parameter.
// The package param is kept last to match the conventional primary_params order.
func (it *Interpretation) SetPackage(v string) {
	v = strings.TrimSpace(v)
	out := make([]Param, 0, len(it.Params)+1)
	for _, p := range it.Params {
		if p.Slug != PackageSlug {
			out = append(out, p)
		}
	}
	if v != "" {
		out = append(out, Param{Slug: PackageSlug, Raw: v})
	}
	if len(out) == 0 {
		out = nil
	}
	it.Params = out
}

// flagClassFootprintConflict marks a refdes/footprint class disagreement.
const flagClassFootprintConflict = "class_footprint_conflict"

var ignoreValueRe = regexp.MustCompile(`(?i)^(dnp|dni|dnf|nc|nf|do[ _]?not[ _]?(populate|place|fit)|not[ _]?fitted|logo|~)$`)

// Interpret parses a BOM line into a structured Interpretation.
func Interpret(in Input) (it Interpretation) {
	it = Interpretation{Confidence: 1.0}

	refClass, hasRefClass := ClassifyRefdes(in.Refdes)
	pkg, mounting, fpClass, _ := CanonicalPackage(in.Footprint)
	it.Mounting = mounting

	// The footprint package (or one leaking from the value field) is stored as the
	// "package" param at every exit, so it rides along regardless of route.
	var valuePkg string
	defer func() {
		p := pkg
		if p == "" {
			p = valuePkg
		}
		it.SetPackage(p)
	}()

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

	// A generic device-type / symbol name in the value (e.g. "LED",
	// "FerriteBead_Small", "Conn_01x04_Pin") is a commodity placeholder, not an MPN.
	// We know the class and (usually) package but no specific part, so route to
	// review and let the LLM look — never report a confident "specific" match.
	if looksLikeGenericPlaceholder(value) {
		it.Route = RouteUnknown
		it.Confidence = 0.3
		it.Escalate = true
		return it
	}

	params, vp, mpnLike, residue := ParseValue(prof, value)
	valuePkg = vp
	it.Params = params
	if pkg == "" && valuePkg != "" && it.Mounting == "" {
		it.Mounting = "smd"
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
	// magnitude (e.g. "9k10%"), or a class conflict — is fair game for the LLM.
	// Identity classes carry an MPN in the value, so leftover tokens there are
	// expected, not residue.
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

package genspec

import (
	"encoding/json"
	"testing"
)

// TestInterpretationLegacyPackageFold verifies a legacy interpretation (package as
// a top-level field) is folded into the params on unmarshal, so old persisted /
// remembered interpretations stay matchable under the package-as-param model.
func TestInterpretationLegacyPackageFold(t *testing.T) {
	const legacy = `{"route":"parametric","class":"R","category":"resistors","package":"0402","params":[{"slug":"resistance","raw":"392k","value_si":392000}]}`
	var it Interpretation
	if err := json.Unmarshal([]byte(legacy), &it); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if it.PackageValue() != "0402" {
		t.Errorf("PackageValue() = %q, want %q", it.PackageValue(), "0402")
	}
	// New-format JSON (package already a param) must not double-add.
	const modern = `{"route":"parametric","category":"resistors","params":[{"slug":"resistance","raw":"392k","value_si":392000},{"slug":"package","raw":"0402","value_si":null}]}`
	var it2 Interpretation
	if err := json.Unmarshal([]byte(modern), &it2); err != nil {
		t.Fatalf("unmarshal modern: %v", err)
	}
	n := 0
	for _, p := range it2.Params {
		if p.Slug == PackageSlug {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly one package param, got %d", n)
	}
}

func TestInterpret(t *testing.T) {
	tests := []struct {
		name      string
		in        Input
		wantRoute Route
		wantCat   string
		wantPkg   string
		wantParam map[string]bool // slugs expected present
	}{
		{
			name:      "generic resistor",
			in:        Input{Refdes: "R1, R2, R3", Footprint: "R_0402_1005Metric", Value: "10k"},
			wantRoute: RouteParametric, wantCat: "resistors", wantPkg: "0402",
			wantParam: map[string]bool{"resistance": true},
		},
		{
			name:      "fully specified cap precluded-from-generic",
			in:        Input{Refdes: "C13", Footprint: "C_0603_1608Metric", Value: "100nF 16V X7R 10%"},
			wantRoute: RouteParametric, wantCat: "capacitors", wantPkg: "0603",
			wantParam: map[string]bool{"capacitance": true, "voltage_rating": true, "dielectric": true, "tolerance": true},
		},
		{
			name:      "metric trap + rkm",
			in:        Input{Refdes: "R7", Footprint: "R_0201_0603Metric", Value: "4k7"},
			wantRoute: RouteParametric, wantCat: "resistors", wantPkg: "0201",
			wantParam: map[string]bool{"resistance": true},
		},
		{
			name:      "ic with mpn in value → identity",
			in:        Input{Refdes: "U5", Footprint: "Package_SO:SOIC-8_3.9x4.9mm_P1.27mm", Value: "LM358"},
			wantRoute: RouteIdentity, wantCat: "integrated-circuits-ics", wantPkg: "SOIC-8",
		},
		{
			name:      "footprint refines D to LED",
			in:        Input{Refdes: "D2", Footprint: "LED_0805_2012Metric", Value: "Green"},
			wantRoute: RouteParametric, wantCat: "optoelectronics", wantPkg: "0805",
			wantParam: map[string]bool{"color": true},
		},
		{
			name:      "LED symbol placeholder → review (not specific)",
			in:        Input{Refdes: "D1", Footprint: "LED_SMD:LED_0402_1005Metric", Value: "LED"},
			wantRoute: RouteUnknown, wantCat: "optoelectronics", wantPkg: "0402",
		},
		{
			name:      "ferrite bead symbol placeholder → review",
			in:        Input{Refdes: "FB1", Footprint: "Inductor_SMD:L_1812_4532Metric", Value: "FerriteBead_Small"},
			wantRoute: RouteUnknown, wantCat: "filters", wantPkg: "1812",
		},
		{
			name:      "connector symbol placeholder → review",
			in:        Input{Refdes: "J1", Footprint: "Connector_PinHeader_2.54mm:PinHeader_1x04_P2.54mm_Vertical", Value: "Conn_01x04_Pin"},
			wantRoute: RouteUnknown, wantCat: "connectors-interconnects",
		},
		{
			name:      "real part number with device word stays identity",
			in:        Input{Refdes: "D1", Footprint: "LED_0805_2012Metric", Value: "WS2812B"},
			wantRoute: RouteIdentity, wantCat: "optoelectronics", wantPkg: "0805",
		},
		{
			name:      "explicit MPN column wins",
			in:        Input{Refdes: "R1", Footprint: "R_0402_1005Metric", Value: "10k", MPN: "RC0402FR-0710KL"},
			wantRoute: RouteIdentity, wantCat: "resistors", wantPkg: "0402",
		},
		{
			name:      "DNP marker → ignore",
			in:        Input{Refdes: "R5", Value: "DNP"},
			wantRoute: RouteIgnore, wantCat: "resistors",
		},
		{
			name:      "test point → ignore",
			in:        Input{Refdes: "TP1"},
			wantRoute: RouteIgnore,
		},
		{
			name:      "value but no class → unknown",
			in:        Input{Value: "10k"},
			wantRoute: RouteUnknown,
		},
		{
			name:      "resistor with no value → unknown",
			in:        Input{Refdes: "R1"},
			wantRoute: RouteUnknown, wantCat: "resistors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := Interpret(tt.in)
			if it.Route != tt.wantRoute {
				t.Errorf("route = %q, want %q (%+v)", it.Route, tt.wantRoute, it)
			}
			if tt.wantCat != "" && it.Category != tt.wantCat {
				t.Errorf("category = %q, want %q", it.Category, tt.wantCat)
			}
			if tt.wantPkg != "" && it.PackageValue() != tt.wantPkg {
				t.Errorf("package = %q, want %q", it.PackageValue(), tt.wantPkg)
			}
			for slug := range tt.wantParam {
				if _, ok := findParam(it.Params, slug); !ok {
					t.Errorf("missing expected param %q (got %+v)", slug, it.Params)
				}
			}
		})
	}
}

// TestInterpretEscalate pins the residue/escalation signal that gates the LLM
// fallback: clean parametric parses do not escalate; weird/partial ones do.
func TestInterpretEscalate(t *testing.T) {
	tests := []struct {
		name         string
		in           Input
		wantEscalate bool
	}{
		// Clean — must NOT escalate (we don't want to pay the LLM for these).
		{"clean rkm", Input{Refdes: "R1", Footprint: "R_0402_1005Metric", Value: "9k"}, false},
		{"clean with tolerance", Input{Refdes: "R1", Value: "10k 10%"}, false},
		{"fully specified cap", Input{Refdes: "C1", Footprint: "C_0603_1608Metric", Value: "100nF 16V X7R 10%"}, false},
		{"led colour", Input{Refdes: "D1", Footprint: "LED_0805_2012Metric", Value: "Red"}, false},
		{"mpn in value, identity class", Input{Refdes: "U1", Value: "LM358"}, false},
		{"explicit mpn column", Input{Refdes: "R1", Value: "10k", MPN: "RC0402FR-0710KL"}, false},
		{"dnp", Input{Refdes: "R5", Value: "DNP"}, false},
		{"resistor no value", Input{Refdes: "R1"}, false},

		// Weird — MUST escalate.
		{"missing space", Input{Refdes: "R1", Value: "9k10%"}, true},
		{"led symbol placeholder", Input{Refdes: "D1", Footprint: "LED_0805_2012Metric", Value: "LED"}, true},
		{"ferrite bead symbol placeholder", Input{Refdes: "FB1", Value: "FerriteBead_Small"}, true},
		{"connector symbol placeholder", Input{Refdes: "J1", Value: "Conn_01x04_Pin"}, true},
		{"slash compound", Input{Refdes: "C1", Value: "5V/10uF"}, true},
		{"leftover token", Input{Refdes: "R1", Value: "10k bogus"}, true},
		{"value but no class", Input{Value: "10k"}, true},
		{"class/footprint conflict", Input{Refdes: "R1", Footprint: "C_0603_1608Metric", Value: "10k"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Interpret(tt.in).Escalate; got != tt.wantEscalate {
				t.Errorf("Escalate = %v, want %v", got, tt.wantEscalate)
			}
		})
	}
}

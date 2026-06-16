package genspec

import "testing"

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
			if tt.wantPkg != "" && it.Package != tt.wantPkg {
				t.Errorf("package = %q, want %q", it.Package, tt.wantPkg)
			}
			for slug := range tt.wantParam {
				if _, ok := findParam(it.Params, slug); !ok {
					t.Errorf("missing expected param %q (got %+v)", slug, it.Params)
				}
			}
		})
	}
}

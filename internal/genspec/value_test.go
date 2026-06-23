package genspec

import (
	"math"
	"testing"
)

func prof(class string) Profile {
	p, ok := ProfileForClass(class)
	if !ok {
		panic("no profile for " + class)
	}
	return p
}

func findParam(params []Param, slug string) (Param, bool) {
	for _, p := range params {
		if p.Slug == slug {
			return p, true
		}
	}
	return Param{}, false
}

func TestParseValue(t *testing.T) {
	type want struct {
		slug string
		si   float64 // NaN => expect no SI value (enum) ; only checked if present
	}
	tests := []struct {
		name    string
		class   string
		value   string
		wants   []want
		mpnLike bool
	}{
		// Resistors
		{"R plain", "R", "5k", []want{{"resistance", 5000}}, false},
		{"R rkm", "R", "4k7", []want{{"resistance", 4700}}, false},
		{"R rkm ohms", "R", "0R22", []want{{"resistance", 0.22}}, false},
		{"R meg", "R", "1M5", []want{{"resistance", 1.5e6}}, false},
		{"R bare", "R", "49.9", []want{{"resistance", 49.9}}, false},
		{"R + tol", "R", "10k 1%", []want{{"resistance", 10000}, {"tolerance", 1}}, false},
		{"R slash tol", "R", "10k/1%", []want{{"resistance", 10000}, {"tolerance", 1}}, false},
		{"R pkg + power", "R", "10k 0402 1/16W", []want{{"resistance", 10000}, {"power_rating", 0.0625}}, false},
		{"R power unspaced", "R", "0.1 1/2W", []want{{"resistance", 0.1}, {"power_rating", 0.5}}, false},
		{"R power spaced W", "R", "0.1 1/2 W", []want{{"resistance", 0.1}, {"power_rating", 0.5}}, false},
		// Capacitors
		{"C nano", "C", "100n", []want{{"capacitance", 100e-9}}, false},
		{"C rkm", "C", "4u7", []want{{"capacitance", 4.7e-6}}, false},
		{"C + dielectric", "C", "10 nF X7R", []want{{"capacitance", 10e-9}, {"dielectric", math.NaN()}}, false},
		{"C + volt", "C", "1uF 25V", []want{{"capacitance", 1e-6}, {"voltage_rating", 25}}, false},
		{"C slash volt prefix", "C", "10u/25V", []want{{"capacitance", 10e-6}, {"voltage_rating", 25}}, false},
		{"C slash volt unit", "C", "10uF/25V", []want{{"capacitance", 10e-6}, {"voltage_rating", 25}}, false},
		{"C slash three", "C", "10uF/25V X7R", []want{{"capacitance", 10e-6}, {"voltage_rating", 25}, {"dielectric", math.NaN()}}, false},
		{"C three", "C", "100µF 16V 20%", []want{{"capacitance", 100e-6}, {"voltage_rating", 16}, {"tolerance", 20}}, false},
		// Inductors — same RKM text as the cap, different unit by class
		{"L rkm", "L", "4n7", []want{{"inductance", 4.7e-9}}, false},
		// Crystals
		{"Y freq", "Y", "16MHz", []want{{"frequency", 16e6}}, false},
		// LED colour (identity class, but an enum secondary makes it parametric)
		{"LED color", "LED", "Red", []want{{"color", math.NaN()}}, false},
		{"LED mpn", "LED", "WS2812B", nil, true},
		// MPN-as-value on parametric and identity classes
		{"C mpn", "C", "GRM188R71C104KA01D", nil, true},
		{"U mpn", "U", "LM358", nil, true},
		{"D mpn", "D", "1N4148", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, pkg, mpnLike, _ := ParseValue(prof(tt.class), tt.value)
			if mpnLike != tt.mpnLike {
				t.Fatalf("mpnLike = %v, want %v (params=%+v)", mpnLike, tt.mpnLike, params)
			}
			for _, w := range tt.wants {
				got, ok := findParam(params, w.slug)
				if !ok {
					t.Errorf("missing param %q (got %+v)", w.slug, params)
					continue
				}
				if math.IsNaN(w.si) {
					if got.ValueSI != nil {
						t.Errorf("param %q: got SI %v, want none", w.slug, *got.ValueSI)
					}
				} else if got.ValueSI == nil {
					t.Errorf("param %q: got no SI, want %v", w.slug, w.si)
				} else if math.Abs(*got.ValueSI-w.si)/math.Max(math.Abs(w.si), 1e-18) > 1e-6 {
					t.Errorf("param %q: got SI %v, want %v", w.slug, *got.ValueSI, w.si)
				}
			}
			_ = pkg
		})
	}
}

func TestParseValuePackageLeak(t *testing.T) {
	_, pkg, _, _ := ParseValue(prof("R"), "10k 0402 1/16W")
	if pkg != "0402" {
		t.Errorf("pkg = %q, want 0402", pkg)
	}
}

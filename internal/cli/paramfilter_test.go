package cli

import (
	"math"
	"net/url"
	"strings"
	"testing"
)

func testRefData() *refData {
	sym := func(s string) *string { return &s }
	return &refData{
		Params: []ParamDef{
			{ID: 7, Slug: "resistance", DisplayName: "Resistance", UnitSymbol: sym("Ω"), ValueType: "numeric"},
			{ID: 8, Slug: "tolerance", DisplayName: "Tolerance", UnitSymbol: sym("%"), ValueType: "numeric"},
			{ID: 9, Slug: "capacitance", DisplayName: "Capacitance", UnitSymbol: sym("F"), ValueType: "numeric"},
			{ID: 11, Slug: "mounting_type", DisplayName: "Mounting Type", ValueType: "enum"},
			{ID: 12, Slug: "features", DisplayName: "Features", ValueType: "enum_set"},
			{ID: 13, Slug: "operating_temperature", DisplayName: "Operating Temperature", ValueType: "range"},
		},
		Categories: []Category{
			{ID: 1, Slug: "passive", DisplayName: "Passive"},
			{ID: 2, Slug: "resistors", DisplayName: "Resistors", ParentID: int32p(1),
				ValueParam:    sp("resistance"),
				PrimaryParams: []string{"resistance", "tolerance", "power_rating", "package"}},
			{ID: 3, Slug: "chip-resistor", DisplayName: "Chip Resistor", ParentID: int32p(2)},
			{ID: 4, Slug: "capacitors", DisplayName: "Capacitors", ParentID: int32p(1)},
		},
	}
}

func int32p(v int32) *int32 { return &v }

func closeEnough(got, want float64) bool {
	if want == 0 {
		return math.Abs(got) < 1e-12
	}
	return math.Abs(got-want)/math.Abs(want) < 1e-9
}

func TestParseParamSpecNumeric(t *testing.T) {
	rd := testRefData()
	tests := []struct {
		name     string
		spec     string
		tol      float64
		wantMin  float64
		wantMax  float64
		hasMin   bool
		hasMax   bool
		paramKey string
	}{
		{name: "bare value gets a band", spec: "resistance=10k", tol: 1,
			wantMin: 9900, wantMax: 10100, hasMin: true, hasMax: true, paramKey: "param_7"},
		{name: "zero tolerance is exact", spec: "resistance=10k", tol: 0,
			wantMin: 10000, wantMax: 10000, hasMin: true, hasMax: true, paramKey: "param_7"},
		{name: "explicit range", spec: "resistance=9k..11k", tol: 1,
			wantMin: 9000, wantMax: 11000, hasMin: true, hasMax: true, paramKey: "param_7"},
		{name: "open upper bound", spec: "resistance=>=10k", tol: 1,
			wantMin: 10000, hasMin: true, paramKey: "param_7"},
		{name: "open lower bound", spec: "resistance=<=1M", tol: 1,
			wantMax: 1e6, hasMax: true, paramKey: "param_7"},
		{name: "range with open upper", spec: "resistance=10k..", tol: 1,
			wantMin: 10000, hasMin: true, paramKey: "param_7"},
		{name: "range with open lower", spec: "resistance=..10k", tol: 1,
			wantMax: 10000, hasMax: true, paramKey: "param_7"},
		{name: "rkm notation", spec: "resistance=4k7", tol: 0,
			wantMin: 4700, wantMax: 4700, hasMin: true, hasMax: true, paramKey: "param_7"},
		{name: "ohm marker rkm", spec: "resistance=0R22", tol: 0,
			wantMin: 0.22, wantMax: 0.22, hasMin: true, hasMax: true, paramKey: "param_7"},
		{name: "percent tolerance", spec: "tolerance=1%", tol: 0,
			wantMin: 1, wantMax: 1, hasMin: true, hasMax: true, paramKey: "param_8"},
		{name: "sub-unit prefix", spec: "capacitance=100n", tol: 0,
			wantMin: 1e-7, wantMax: 1e-7, hasMin: true, hasMax: true, paramKey: "param_9"},
		{name: "negative range", spec: "operating_temperature=-40..85", tol: 1,
			wantMin: -40, wantMax: 85, hasMin: true, hasMax: true, paramKey: "param_13"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parseParamSpec(tc.spec, rd, tc.tol)
			if err != nil {
				t.Fatalf("parseParamSpec(%q) failed: %v", tc.spec, err)
			}
			if tc.hasMin {
				if f.Min == nil {
					t.Fatalf("expected a lower bound, got none")
				}
				if !closeEnough(*f.Min, tc.wantMin) {
					t.Errorf("min = %v, want %v", *f.Min, tc.wantMin)
				}
			} else if f.Min != nil {
				t.Errorf("unexpected lower bound %v", *f.Min)
			}
			if tc.hasMax {
				if f.Max == nil {
					t.Fatalf("expected an upper bound, got none")
				}
				if !closeEnough(*f.Max, tc.wantMax) {
					t.Errorf("max = %v, want %v", *f.Max, tc.wantMax)
				}
			} else if f.Max != nil {
				t.Errorf("unexpected upper bound %v", *f.Max)
			}

			q := url.Values{}
			f.apply(q)
			if tc.hasMin && q.Get(tc.paramKey+"_min") == "" {
				t.Errorf("query is missing %s_min", tc.paramKey)
			}
			if tc.hasMax && q.Get(tc.paramKey+"_max") == "" {
				t.Errorf("query is missing %s_max", tc.paramKey)
			}
		})
	}
}

func TestParseParamSpecEnum(t *testing.T) {
	rd := testRefData()

	f, err := parseParamSpec("mounting_type=SMD,Through Hole", rd, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Values) != 2 || f.Values[0] != "SMD" || f.Values[1] != "Through Hole" {
		t.Fatalf("values = %v, want [SMD, Through Hole]", f.Values)
	}
	if f.Min != nil || f.Max != nil {
		t.Errorf("an enum filter must not set numeric bounds")
	}

	q := url.Values{}
	f.apply(q)
	if got := q["param_11_values"]; len(got) != 2 {
		t.Errorf("param_11_values = %v, want two entries", got)
	}

	// enum_set uses the same values path — the server handles the overlap match.
	set, err := parseParamSpec("features=Automotive", rd, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set.Values) != 1 {
		t.Errorf("enum_set values = %v, want one entry", set.Values)
	}
}

func TestParseParamSpecErrors(t *testing.T) {
	rd := testRefData()
	tests := []struct {
		name     string
		spec     string
		wantCode int
	}{
		{"missing equals", "resistance", ExitUsage},
		{"empty value", "resistance=", ExitUsage},
		{"unknown parameter", "flux_capacitance=1.21G", ExitNotFound},
		{"unparseable number", "resistance=banana", ExitUsage},
		{"inverted range", "resistance=10k..1k", ExitUsage},
		{"package is not a parameter", "package=0402", ExitUsage},
		{"empty range", "resistance=..", ExitUsage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseParamSpec(tc.spec, rd, 1)
			if err == nil {
				t.Fatalf("expected an error for %q", tc.spec)
			}
			ee, ok := err.(*ExitError)
			if !ok {
				t.Fatalf("error is not an ExitError: %v", err)
			}
			if ee.Code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (%v)", ee.Code, tc.wantCode, err)
			}
		})
	}
}

// Display names are what `pt show` prints, so they must be accepted as slugs.
func TestParamBySlugAcceptsDisplayName(t *testing.T) {
	rd := testRefData()
	for _, ref := range []string{"resistance", "Resistance", "RESISTANCE", "mounting_type", "Mounting Type"} {
		if def := rd.paramBySlug(ref); def == nil {
			t.Errorf("paramBySlug(%q) found nothing", ref)
		}
	}
	if def := rd.paramBySlug("nope"); def != nil {
		t.Errorf("paramBySlug(\"nope\") = %v, want nil", def)
	}
}

func TestResolveCategory(t *testing.T) {
	rd := testRefData()

	if c, err := rd.resolveCategory("2"); err != nil || c.Slug != "resistors" {
		t.Errorf("by id: %v, %v", c, err)
	}
	if c, err := rd.resolveCategory("resistors"); err != nil || c.ID != 2 {
		t.Errorf("by slug: %v, %v", c, err)
	}
	if c, err := rd.resolveCategory("Capacitors"); err != nil || c.ID != 4 {
		t.Errorf("by display name: %v, %v", c, err)
	}
	// "resistor" is a substring of both Resistors and Chip Resistor.
	_, err := rd.resolveCategory("resistor")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	if ee, ok := err.(*ExitError); !ok || ee.Code != ExitAmbiguous {
		t.Errorf("want ExitAmbiguous, got %v", err)
	}
	if _, err := rd.resolveCategory("inductors"); err == nil {
		t.Error("expected a not-found error")
	}
}

func TestCategoryPath(t *testing.T) {
	rd := testRefData()
	c := rd.categoryByID(3)
	if got, want := rd.path(c), "Passive / Resistors / Chip Resistor"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// The class profile (value_param + primary_params) is curated on the parametric
// class, not on every leaf, and the server reads only the category asked for. A
// leaf must therefore inherit its ancestor's profile rather than report none.
func TestEffectiveProfileInheritsFromClass(t *testing.T) {
	rd := testRefData()

	leaf := rd.categoryByID(3) // Chip Resistor — no profile of its own
	valueParam, primary, from := rd.effectiveProfile(leaf)
	if valueParam != "resistance" {
		t.Errorf("value param = %q, want it inherited from Resistors", valueParam)
	}
	if len(primary) != 4 || primary[0] != "resistance" {
		t.Errorf("primary params = %v, want the class list", primary)
	}
	if from == nil || from.ID != 2 {
		t.Fatalf("profile source = %v, want category 2 (Resistors)", from)
	}

	// A category with its own profile reports itself, not an ancestor.
	_, _, self := rd.effectiveProfile(rd.categoryByID(2))
	if self == nil || self.ID != 2 {
		t.Errorf("a curated category should report itself, got %v", self)
	}

	// Nothing curated anywhere in the ancestry.
	vp, pp, none := rd.effectiveProfile(rd.categoryByID(4))
	if vp != "" || len(pp) != 0 || none != nil {
		t.Errorf("uncurated branch reported a profile: (%q, %v, %v)", vp, pp, none)
	}
}

// A range parameter stores the span's lower bound in numeric_value, and the
// server's filter compares only that — so filtering one must warn rather than
// quietly answer a different question.
func TestRangeParameterFilterWarns(t *testing.T) {
	rd := testRefData()

	f, err := parseParamSpec("operating_temperature=-40..85", rd, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	warn := f.rangeCaveat()
	if warn == "" {
		t.Fatal("filtering a range parameter produced no caveat")
	}
	if !strings.Contains(warn, "lower bound") {
		t.Errorf("caveat does not explain the semantics: %q", warn)
	}

	// Ordinary numeric parameters carry no caveat.
	num, err := parseParamSpec("resistance=10k", rd, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := num.rangeCaveat(); got != "" {
		t.Errorf("numeric parameter warned unnecessarily: %q", got)
	}
}

// Range-typed definitions are dropped by the category parameters endpoint, so
// the CLI has to name them from the registry instead.
func TestRangeParamsFromRegistry(t *testing.T) {
	rd := testRefData()
	ranges := rd.rangeParams()
	if len(ranges) != 1 || ranges[0].Slug != "operating_temperature" {
		t.Errorf("rangeParams = %v, want the one range-typed definition", ranges)
	}
}

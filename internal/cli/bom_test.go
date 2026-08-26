package cli

import (
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/bomfile"
	"github.com/Yamakaja/partrack/internal/genspec"
)

func parseSheet(t *testing.T, text string) *bomfile.Sheet {
	t.Helper()
	sheet, err := bomfile.Parse(strings.NewReader(text), bomfile.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return sheet
}

// A row with no MPN and no SKU is a parametric row. Passing it through the
// display helper `str`, which renders an em-dash for absent, made every row look
// like it carried an identity hint — genspec then routed all of them to identity
// resolution and reported no parameters at all, while looking entirely
// plausible. This pins the deref.
func TestRowsWithoutIdentityStayParametric(t *testing.T) {
	sheet := parseSheet(t, "Ref,Qty,Value,Footprint\nR1,1,10k,R_0603_1608Metric\n")
	build := bomfile.BuildItems(sheet, bomfile.AutoMapHeaders(sheet.Headers), bomfile.BuildOptions{})

	rows := buildBOMRows(build)
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Route != genspec.RouteParametric {
		t.Fatalf("route = %q, want parametric — an absent MPN must not read as an identity hint", rows[0].Route)
	}
	if rows[0].MPN != "" {
		t.Errorf("MPN = %q, want empty", rows[0].MPN)
	}
	var resistance string
	for _, p := range rows[0].Params {
		if p.Slug == "resistance" {
			resistance = p.Raw
		}
	}
	if resistance != "10k" {
		t.Errorf("resistance not extracted; params = %+v", rows[0].Params)
	}
}

// A real MPN column must still win, or the check would misreport identity BOMs
// in the other direction.
func TestIdentityRowsAreRoutedToIdentity(t *testing.T) {
	sheet := parseSheet(t, "Ref,Qty,MPN,Value\nR1,1,RC0402FR-0710KL,10k\n")
	build := bomfile.BuildItems(sheet, bomfile.AutoMapHeaders(sheet.Headers), bomfile.BuildOptions{})

	rows := buildBOMRows(build)
	if rows[0].Route != genspec.RouteIdentity {
		t.Errorf("route = %q, want identity", rows[0].Route)
	}
	if rows[0].MPN != "RC0402FR-0710KL" {
		t.Errorf("MPN = %q", rows[0].MPN)
	}
}

func TestApplyMappingsByNameAndPosition(t *testing.T) {
	sheet := parseSheet(t, "Designator,Count,Comment,Package\nR1,1,10k,0603\n")
	roles := bomfile.AutoMapHeaders(sheet.Headers)

	// "Comment" is Altium's value column; nothing auto-maps it.
	if roles[2] != bomfile.RoleIgnore {
		t.Fatalf("expected Comment to start unmapped, got %q", roles[2])
	}

	got, err := applyMappings(sheet, roles, []string{"Comment=value", "4=footprint"})
	if err != nil {
		t.Fatalf("applyMappings: %v", err)
	}
	if got[2] != bomfile.RoleValue {
		t.Errorf("by name: column 3 = %q, want value", got[2])
	}
	if got[3] != bomfile.RoleFootprint {
		t.Errorf("by position: column 4 = %q, want footprint", got[3])
	}
}

func TestApplyMappingsRejectsBadInput(t *testing.T) {
	sheet := parseSheet(t, "Ref,Qty\nR1,1\n")
	roles := bomfile.AutoMapHeaders(sheet.Headers)

	tests := []struct {
		spec string
		code int
	}{
		{"Ref", ExitUsage},          // no '='
		{"Ref=nonsense", ExitUsage}, // unknown role
		{"9=value", ExitUsage},      // column out of range
		{"Missing=value", ExitNotFound},
	}
	for _, tc := range tests {
		_, err := applyMappings(sheet, roles, []string{tc.spec})
		if err == nil {
			t.Errorf("--map %q should have failed", tc.spec)
			continue
		}
		ee, ok := err.(*ExitError)
		if !ok || ee.Code != tc.code {
			t.Errorf("--map %q: want exit %d, got %v", tc.spec, tc.code, err)
		}
	}
}

// A comma-bearing --map value must survive; the role names never contain
// commas, but the shared list-splitting helper does split on them.
func TestSingleRune(t *testing.T) {
	tests := []struct {
		in   string
		want rune
		ok   bool
	}{
		{"", 0, true},
		{";", ';', true},
		{"tab", '\t', true},
		{`\t`, '\t', true},
		{"##", 0, false},
	}
	for _, tc := range tests {
		got, err := singleRune("--delimiter", tc.in)
		if tc.ok && err != nil {
			t.Errorf("singleRune(%q) errored: %v", tc.in, err)
			continue
		}
		if !tc.ok {
			if err == nil {
				t.Errorf("singleRune(%q) should have failed", tc.in)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("singleRune(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Rows needing attention lead, so the wall of confident rows never buries the
// three that are wrong.
func TestSelectRowsOrdersAttentionFirst(t *testing.T) {
	prev := bomCheckFlags.problemsOnly
	t.Cleanup(func() { bomCheckFlags.problemsOnly = prev })
	bomCheckFlags.problemsOnly = false

	rows := []bomRow{
		{Line: 1, Interpretation: genspec.Interpretation{Route: genspec.RouteParametric}},
		{Line: 2, Interpretation: genspec.Interpretation{Route: genspec.RouteUnknown}},
		{Line: 3, Interpretation: genspec.Interpretation{Route: genspec.RouteParametric}},
		{Line: 4, Interpretation: genspec.Interpretation{Route: genspec.RouteParametric, Escalate: true}},
	}
	got := selectRows(rows)
	want := []int{2, 4, 1, 3}
	for i, w := range want {
		if got[i].Line != w {
			t.Errorf("position %d = line %d, want %d", i, got[i].Line, w)
		}
	}

	bomCheckFlags.problemsOnly = true
	got = selectRows(rows)
	if len(got) != 2 {
		t.Errorf("--problems kept %d rows, want 2", len(got))
	}
}

// The value column shows what the user wrote; when there is no value the MPN or
// SKU is the identifying text, and only a truly bare row falls back to the dash.
func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "MPN-1", dash); got != "MPN-1" {
		t.Errorf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", dash); got != dash {
		t.Errorf("firstNonEmpty = %q, want the dash", got)
	}
}

// Units come from the class profile for the primary magnitude and from the
// secondary table for the rest — a bare "16" for a voltage rating is a
// materially different claim from "16 V".
func TestFormatParamsCarriesUnits(t *testing.T) {
	v := func(f float64) *float64 { return &f }
	r := bomRow{Interpretation: genspec.Interpretation{
		Class: "C",
		Params: []genspec.Param{
			{Slug: "capacitance", Raw: "100nF", ValueSI: v(100e-9)},
			{Slug: "voltage_rating", Raw: "16V", ValueSI: v(16)},
			{Slug: "dielectric", Raw: "X7R"},
			{Slug: genspec.PackageSlug, Raw: "0402"},
		},
	}}
	got := formatParams(r)
	for _, want := range []string{"capacitance=100 nF", "voltage_rating=16 V", "dielectric=X7R"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatParams = %q, missing %q", got, want)
		}
	}
	// Package has its own column and must not be repeated here.
	if strings.Contains(got, "package") {
		t.Errorf("package should not appear in the params column: %q", got)
	}
}

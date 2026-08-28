package cli

import (
	"strings"
	"testing"
)

func sp(s string) *string { return &s }

func TestResolveFieldsOrderAndAliases(t *testing.T) {
	cols, err := resolveFields([]string{"stock", "mpn", "manufacturer"}, partColumns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := []string{cols[0].Key, cols[1].Key, cols[2].Key}
	want := []string{"stock", "mpn", "mfr"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, got[i], want[i])
		}
	}

	if _, err := resolveFields([]string{"nonsense"}, partColumns); err == nil {
		t.Error("expected an error for an unknown field")
	}
}

func TestSplitList(t *testing.T) {
	got := splitList([]string{"a,b", " c ", "", "d,,e"})
	want := []string{"a", "b", "c", "d", "e"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("splitList = %v, want %v", got, want)
	}
}

// --param values may contain commas (mounting=SMD,THT is one filter), so the
// param flag must not be comma-split the way other list flags are.
func TestSplitParamSpecsKeepsCommas(t *testing.T) {
	got := splitParamSpecs([]string{"mounting=SMD,THT", " resistance=10k "})
	if len(got) != 2 {
		t.Fatalf("got %d specs, want 2: %v", len(got), got)
	}
	if got[0] != "mounting=SMD,THT" {
		t.Errorf("spec was split on the comma: %q", got[0])
	}
}

// An untracked part has no inventory entry, which is a different statement from
// holding zero — the two must not render alike.
func TestStockColumnDistinguishesUntrackedFromZero(t *testing.T) {
	col, _ := lookupColumn("stock", partColumns)
	zero := int32(0)

	untracked := PartListItem{}
	if got := col.Value(untracked); got != dash {
		t.Errorf("untracked stock = %q, want %q", got, dash)
	}

	tracked := PartListItem{InventoryEntryID: sp("entry-id"), QuantityOnHand: &zero}
	if got := col.Value(tracked); got != "0" {
		t.Errorf("tracked-but-empty stock = %q, want \"0\"", got)
	}
}

func TestFormatLocation(t *testing.T) {
	tests := []struct {
		label *string
		coord []int32
		want  string
	}{
		{nil, nil, dash},
		{sp(""), nil, dash},
		{sp("Drawer A"), nil, "Drawer A"},
		{sp("Drawer A"), []int32{3}, "Drawer A/3"},
		{sp("Shelf"), []int32{2, 4}, "Shelf/2,4"},
	}
	for _, tc := range tests {
		if got := formatLocation(tc.label, tc.coord); got != tc.want {
			t.Errorf("formatLocation(%v, %v) = %q, want %q", tc.label, tc.coord, got, tc.want)
		}
	}
}

func TestNormalizeStockStatus(t *testing.T) {
	tests := []struct{ in, want string }{
		{"in", "in_stock"},
		{"low", "low_stock"},
		{"out", "out_of_stock"},
		{"untracked", "not_tracked"},
		{"in,low", "in_stock,low_stock"},
		{"in_stock", "in_stock"},
	}
	for _, tc := range tests {
		if got := normalizeStockStatus([]string{tc.in}); got != tc.want {
			t.Errorf("normalizeStockStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := normalizeStockStatus(nil); got != "" {
		t.Errorf("empty input produced %q", got)
	}
}

func TestNormalizeLifecycle(t *testing.T) {
	if got := normalizeLifecycle([]string{"nla", "NRND"}); got != "no_longer_available,nrnd" {
		t.Errorf("normalizeLifecycle = %q", got)
	}
	if got := normalizeLifecycle([]string{"no-longer-available"}); got != "no_longer_available" {
		t.Errorf("hyphenated form = %q", got)
	}
}

// Sorting must be numeric for quantity columns, and absent values must sink to
// the bottom in either direction rather than reading as zero.
func TestSortItemsNumeric(t *testing.T) {
	n := func(v int32) *int32 { return &v }
	items := []PartListItem{
		{Mpn: sp("b"), QuantityOnHand: n(9)},
		{Mpn: sp("a"), QuantityOnHand: n(100)},
		{Mpn: sp("c")},
		{Mpn: sp("d"), QuantityOnHand: n(50)},
	}
	if err := sortItems(items, "stock:desc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a", "d", "b", "c"}
	for i, w := range want {
		if got := *items[i].Mpn; got != w {
			t.Errorf("desc position %d = %q, want %q", i, got, w)
		}
	}

	if err := sortItems(items, "stock"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = []string{"b", "d", "a", "c"}
	for i, w := range want {
		if got := *items[i].Mpn; got != w {
			t.Errorf("asc position %d = %q, want %q", i, got, w)
		}
	}

	if err := sortItems(items, "bogus"); err == nil {
		t.Error("expected an error for an unknown sort field")
	}
}

func TestParamColumns(t *testing.T) {
	items := []PartListItem{
		{Parameters: map[string]string{"resistance": "10 kΩ", "tolerance": "±1%"}},
		{Parameters: map[string]string{"resistance": "4.7 kΩ", "power": "1/16 W"}},
	}
	all := paramColumns(items, nil)
	if strings.Join(all, ",") != "power,resistance,tolerance" {
		t.Errorf("bare --params = %v, want every slug sorted", all)
	}

	subset := paramColumns(items, []string{"Tolerance", "resistance"})
	if strings.Join(subset, ",") != "resistance,tolerance" {
		t.Errorf("named subset = %v", subset)
	}

	if got := paramColumns(items, []string{"missing"}); len(got) != 0 {
		t.Errorf("unknown slug produced columns: %v", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate kept %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcd…" {
		t.Errorf("truncate = %q, want %q", got, "abcd…")
	}
	// Multi-byte runes must not be cut mid-character.
	if got := truncate("10 kΩ ±1% résistance", 8); len([]rune(got)) != 8 {
		t.Errorf("truncate produced %d runes: %q", len([]rune(got)), got)
	}
}

package substitute

import "testing"

func TestFreeStock(t *testing.T) {
	cases := []struct {
		onHand, reserved, want int32
	}{
		{10, 3, 7},
		{5, 5, 0},
		{2, 5, 0}, // over-reserved floors at zero
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := FreeStock(c.onHand, c.reserved); got != c.want {
			t.Errorf("FreeStock(%d,%d) = %d, want %d", c.onHand, c.reserved, got, c.want)
		}
	}
}

func TestSelect(t *testing.T) {
	items := []Item{
		{LineItemID: "primary-ok", PrimaryID: "P1", Required: 10},
		{LineItemID: "use-sub", PrimaryID: "P2", Required: 10},
		{LineItemID: "sub-short", PrimaryID: "P3", Required: 10},
		{LineItemID: "priority", PrimaryID: "P4", Required: 10},
		{LineItemID: "unresolved", PrimaryID: "", Required: 10},
		{LineItemID: "zero", PrimaryID: "P5", Required: 0},
	}
	subsByItem := map[string][]string{
		"use-sub":   {"S2"},
		"sub-short": {"S3"},
		// S4a is higher priority but short; S4b fully covers and must win.
		"priority": {"S4a", "S4b"},
	}
	onHand := map[string]int32{
		"P1": 10, // primary covers → no substitute
		"P2": 2, "S2": 10, // primary short, sub covers
		"P3": 2, "S3": 5, // both short → no selection
		"P4": 0, "S4a": 4, "S4b": 12,
		"P5": 0,
	}
	reserved := map[string]int32{
		"S2": 0,
	}

	got := Select(items, subsByItem, onHand, reserved)

	if _, ok := got["primary-ok"]; ok {
		t.Errorf("primary-ok should not select a substitute")
	}
	if got["use-sub"] != "S2" {
		t.Errorf("use-sub = %q, want S2", got["use-sub"])
	}
	if _, ok := got["sub-short"]; ok {
		t.Errorf("sub-short should not select an insufficient substitute")
	}
	if got["priority"] != "S4b" {
		t.Errorf("priority = %q, want S4b (first that fully covers)", got["priority"])
	}
	if _, ok := got["unresolved"]; ok {
		t.Errorf("unresolved line item should be skipped")
	}
	if _, ok := got["zero"]; ok {
		t.Errorf("zero-demand line item should be skipped")
	}
}

func TestSelectRespectsReservations(t *testing.T) {
	// Substitute has on-hand 10 but 6 reserved by other builds → only 4 free,
	// below the required 5, so it must not be selected.
	items := []Item{{LineItemID: "li", PrimaryID: "P", Required: 5}}
	subsByItem := map[string][]string{"li": {"S"}}
	onHand := map[string]int32{"P": 0, "S": 10}
	reserved := map[string]int32{"S": 6}

	if got := Select(items, subsByItem, onHand, reserved); len(got) != 0 {
		t.Errorf("expected no selection when substitute free stock is insufficient, got %v", got)
	}
}

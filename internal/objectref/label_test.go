package objectref

import "testing"

func strp(s string) *string { return &s }

func TestOrderLabel(t *testing.T) {
	const id = "0f3a9c21-1111-2222-3333-444455556666"
	cases := []struct {
		name   string
		distr  string
		po     *string
		extRef *string
		want   string
	}{
		{"purchase order wins", "DigiKey", strp("PO-1042"), strp("WEB-88213"), "DigiKey #PO-1042"},
		{"falls back to external reference", "Mouser", nil, strp("WEB-88213"), "Mouser #WEB-88213"},
		{"falls back to short id", "LCSC", nil, nil, "LCSC 0f3a9c21"},
		{"blank references fall through", "LCSC", strp("  "), strp(""), "LCSC 0f3a9c21"},
		{"does not double a typed '#'", "DigiKey", strp("#1042"), nil, "DigiKey #1042"},
		{"trims surrounding space", "DigiKey", strp("  PO-7  "), nil, "DigiKey #PO-7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OrderLabel(c.distr, c.po, c.extRef, id); got != c.want {
				t.Errorf("OrderLabel() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRevisionLabel(t *testing.T) {
	cases := []struct {
		project, revision, want string
	}{
		{"Blinky", "B", "Blinky rev B"},
		{"Blinky", "v1.2", "Blinky v1.2"},   // "v" prefix not doubled
		{"Blinky", "Rev C", "Blinky Rev C"}, // "rev" prefix not doubled
		{"Blinky", "", "Blinky"},            // no revision label recorded
		{"", "B", "rev B"},                  // no project name recorded
		{"Blinky", "  B  ", "Blinky rev B"}, // trimmed
	}
	for _, c := range cases {
		if got := RevisionLabel(c.project, c.revision); got != c.want {
			t.Errorf("RevisionLabel(%q, %q) = %q, want %q", c.project, c.revision, got, c.want)
		}
	}
}

func TestPartLabel(t *testing.T) {
	cases := []struct {
		name       string
		mpn, descr *string
		want       string
	}{
		{"mpn wins", strp("STM32F103C8T6"), strp("MCU"), "STM32F103C8T6"},
		{"generic part falls back to description", nil, strp("0603 10nF 50V X7R"), "0603 10nF 50V X7R"},
		{"blank mpn falls through", strp("  "), strp("MCU"), "MCU"},
		{"nothing to show", nil, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PartLabel(c.mpn, c.descr); got != c.want {
				t.Errorf("PartLabel() = %q, want %q", got, c.want)
			}
		})
	}
}

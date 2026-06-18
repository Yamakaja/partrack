package genericpart

import "testing"

func num(f float64) *float64 { return &f }

func TestSynthesize(t *testing.T) {
	resOrder := []string{"resistance", "tolerance", "power_rating", "package"}

	t.Run("resistor value only", func(t *testing.T) {
		id, ok := Synthesize("RES", "resistance", resOrder, "Resistors", "0402", []Param{
			{Slug: "resistance", Numeric: num(1), UnitSymbol: "Ω"},
		})
		if !ok {
			t.Fatal("expected ok")
		}
		if id.MPN != "GEN-RES-1R-0402" {
			t.Errorf("MPN = %q, want GEN-RES-1R-0402", id.MPN)
		}
		if id.Description != "1 Ω 0402 Resistor" {
			t.Errorf("Description = %q, want \"1 Ω 0402 Resistor\"", id.Description)
		}
	})

	t.Run("resistor with tolerance", func(t *testing.T) {
		id, _ := Synthesize("RES", "resistance", resOrder, "Resistors", "0402", []Param{
			{Slug: "resistance", Numeric: num(10000), UnitSymbol: "Ω"},
			{Slug: "tolerance", Numeric: num(1), UnitSymbol: "%"},
		})
		if id.MPN != "GEN-RES-10K-0402-1%" {
			t.Errorf("MPN = %q, want GEN-RES-10K-0402-1%%", id.MPN)
		}
		if id.Description != "10 kΩ ±1% 0402 Resistor" {
			t.Errorf("Description = %q", id.Description)
		}
	})

	t.Run("capacitor", func(t *testing.T) {
		id, _ := Synthesize("CAP", "capacitance",
			[]string{"capacitance", "voltage_rating", "dielectric", "package"},
			"Capacitors", "0603", []Param{
				{Slug: "capacitance", Numeric: num(100e-9), UnitSymbol: "F"},
				{Slug: "dielectric", Value: "X7R"},
			})
		if id.MPN != "GEN-CAP-100N-0603-X7R" {
			t.Errorf("MPN = %q, want GEN-CAP-100N-0603-X7R", id.MPN)
		}
		if id.Description != "100 nF X7R 0603 Capacitor" {
			t.Errorf("Description = %q", id.Description)
		}
	})

	t.Run("no generic code disables synthesis", func(t *testing.T) {
		if _, ok := Synthesize("", "resistance", resOrder, "Resistors", "0402", nil); ok {
			t.Error("expected ok=false when code is empty")
		}
	})

	t.Run("multi-word category noun", func(t *testing.T) {
		id, _ := Synthesize("IND", "inductance",
			[]string{"inductance", "package"},
			"Inductors, Coils, Chokes", "0805", []Param{
				{Slug: "inductance", Numeric: num(2.2e-6), UnitSymbol: "H"},
			})
		if id.MPN != "GEN-IND-2U2-0805" {
			t.Errorf("MPN = %q, want GEN-IND-2U2-0805", id.MPN)
		}
		if id.Description != "2.2 µH 0805 Inductor" {
			t.Errorf("Description = %q", id.Description)
		}
	})
}

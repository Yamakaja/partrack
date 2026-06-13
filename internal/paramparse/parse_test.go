package paramparse

import (
	"math"
	"testing"
)

func approx(a, b float64) bool {
	if b == 0 {
		return math.Abs(a) < 1e-15
	}
	return math.Abs(a-b)/math.Abs(b) < 1e-9
}

func pf(v float64) *float64 { return &v }

func TestParseEnumSet(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"CANbus, EBI/EMI, Ethernet", []string{"CANbus", "EBI/EMI", "Ethernet"}},
		{"Current Limit, Enable", []string{"Current Limit", "Enable"}},
		{"SPI", []string{"SPI"}},
		{"A, A, B", []string{"A", "B"}},    // dedupe
		{" A ,  , B ", []string{"A", "B"}}, // trim + drop empties
		{"", nil},
		{" , ", nil},
	}
	for _, tt := range tests {
		got := ParseEnumSet(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("ParseEnumSet(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ParseEnumSet(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		valueType string
		wantVal   *float64
		wantMax   *float64
	}{
		// Text / enum types — always nil
		{"text type", "0402", "text", nil, nil},
		{"enum type", "SMD", "enum", nil, nil},

		// Unparseable / non-numeric
		{"empty", "", "numeric", nil, nil},
		{"dash", "-", "numeric", nil, nil},
		{"na", "N/A", "numeric", nil, nil},
		// Package codes use value_type="text" per spec; parser returns nil for text.
		{"package code", "0402", "text", nil, nil},

		// Simple numeric values
		{"integer", "100", "numeric", pf(100), nil},
		{"decimal", "3.3", "numeric", pf(3.3), nil},
		{"negative", "-40", "numeric", pf(-40), nil},

		// SI prefix values
		{"kilo ohm", "10 kOhm", "numeric", pf(10000), nil},
		{"kilo ohms", "10 kOhms", "numeric", pf(10000), nil},
		{"nano farad", "100 nF", "numeric", pf(100e-9), nil},
		{"micro farad", "10 µF", "numeric", pf(10e-6), nil},
		{"micro farad u", "10 uF", "numeric", pf(10e-6), nil},
		{"giga hz", "2.4 GHz", "numeric", pf(2.4e9), nil},
		{"milli watt", "0.1W", "numeric", pf(0.1), nil},
		{"milli ohm", "100mOhm", "numeric", pf(0.1), nil},
		{"mega ohm", "1 MOhm", "numeric", pf(1e6), nil},
		{"pico farad", "22 pF", "numeric", pf(22e-12), nil},
		{"kilo plain K", "10K", "numeric", pf(10000), nil},
		{"milimeters", "10mm", "numeric", pf(10e-3), nil},

		// Tolerance
		{"tolerance 1pct", "±1%", "numeric", pf(1.0), nil},
		{"tolerance 5pct", "±5%", "numeric", pf(5.0), nil},
		{"tolerance space", "± 0.5 %", "numeric", pf(0.5), nil},
		{"tolerance 30ppm", "±30ppm", "numeric", pf(30e-4), nil},
		{"tolerance 100ppm", "100 ppm", "numeric", pf(100e-4), nil},
		{"tolerance 500ppb", "±500ppb", "numeric", pf(500e-7), nil},
		{"tolerance 1000ppb", "1000 ppb", "numeric", pf(1000e-7), nil},

		// Ranges (value_type = range)
		{"temp range", "-40°C ~ 85°C", "range", pf(-40), pf(85)},
		{"temp range2", "-55°C ~ 125°C", "range", pf(-55), pf(125)},
		{"resistance range", "10 kOhm ~ 100 kOhm", "range", pf(10000), pf(100000)},

		// Fractions (resistor power ratings etc.)
		{"frac 1/8 W", "1/8W", "numeric", pf(0.125), nil},
		{"frac 1/16 space W", "1/16 W", "numeric", pf(0.0625), nil},
		{"frac 1/4 W", "1/4W", "numeric", pf(0.25), nil},
		{"frac 1/2 space W", "1/2 W", "numeric", pf(0.5), nil},
		{"frac no unit", "3/8", "numeric", pf(0.375), nil},
		{"frac range", "1/8 ~ 1/4", "range", pf(0.125), pf(0.25)},
		{"frac div zero", "1/0", "numeric", nil, nil},

		// Compound units (consistent denominator per parameter assumed): the
		// magnitude is kept with the numerator SI-prefix normalised; the denominator
		// is an opaque label and is not applied.
		{"slew rate no space", "0.6V/µs", "numeric", pf(0.6), nil},
		{"slew rate spaced", "13 V/µs", "numeric", pf(13), nil},
		{"slew rate us", "5 V/us", "numeric", pf(5), nil},
		{"slew rate ms", "600 V/ms", "numeric", pf(600), nil},
		{"current rate", "1.2 A/µs", "numeric", pf(1.2), nil},
		{"thermal resistance", "2.5 °C/W", "numeric", pf(2.5), nil},
		{"tempco mV per C", "10mV/°C", "numeric", pf(0.01), nil},
		// ppm/ppb in a compound unit normalise to percent-equivalent (1 ppm = 1e-4 %,
		// 1 ppb = 1e-7 %) so ppm and ppb values on one parameter stay comparable:
		// 50 ppb/°C (5e-6) correctly sorts below 100 ppm/°C (0.01).
		{"tempco ppm per C", "100 ppm/°C", "numeric", pf(0.01), nil},
		{"tempco ppm per C nospace", "30ppm/°C", "numeric", pf(30e-4), nil},
		{"tempco ppb per C", "50 ppb/°C", "numeric", pf(50e-7), nil},
		{"gain V per V", "100 V/V", "numeric", pf(100), nil},

		// Point / conditional specs: leading value is searchable, "@ conditions"
		// are display-only context (dropped before parsing).
		{"point uA", "40 µA @ 60 V", "numeric", pf(40e-6), nil},
		{"point mV", "280 mV @ 10 mA", "numeric", pf(0.28), nil},
		{"point nC", "23.4 nC @ 8 V", "numeric", pf(23.4e-9), nil},
		{"point mOhm two conds", "380mOhm @ 650mA, 4.5V", "numeric", pf(0.38), nil},
		{"point pF two conds", "17pF @ 0V, 1MHz", "numeric", pf(17e-12), nil},

		// Alternate representations of one value collapse; tuples do not.
		{"alt-rep dec+frac", "0.063W, 1/16W", "numeric", pf(0.063), nil},
		{"tuple not collapsed", "1.8V, 4.5V", "numeric", nil, nil},

		// Voltage
		{"volts", "3.3V", "numeric", pf(3.3), nil},
		{"kilovolt", "1 kV", "numeric", pf(1000), nil},

		// Current
		{"amps", "1.5 A", "numeric", pf(1.5), nil},
		{"milliamps", "500 mA", "numeric", pf(0.5), nil},
		{"microamps_extras", "800µA (x2 Channels)", "numeric", pf(800e-6), nil},

		// Unicode normalisation
		{"omega char", "10 Ω", "numeric", pf(10), nil},
		{"micro sign", "100 \u00b5F", "numeric", pf(100e-6), nil},

		// Storage sizes (stored in bits)
		{"kbit", "1 kbit", "numeric", pf(1e3), nil},
		{"9 Mbit", "9 Mbit", "numeric", pf(9e6), nil},
		{"KB", "1 KB", "numeric", pf(1e3 * 8), nil},
		{"KiB", "1 KiB", "numeric", pf(1024 * 8), nil},
		{"GiB", "1 GiB", "numeric", pf(1024 * 1024 * 1024 * 8), nil},
		{"MB", "1 MB", "numeric", pf(1e6 * 8), nil},
		{"Gbit", "4 Gbit", "numeric", pf(4e9), nil},
		{"TiB", "1 TiB", "numeric", pf(float64(int64(1)<<40) * 8), nil},

		// Dual-unit inch+mm preprocessing
		{"inch mm 0.015", `0.015" (0.37mm)`, "numeric", pf(0.37e-3), nil},
		{"inch mm 0.016", `0.016" (0.40mm)`, "numeric", pf(0.40e-3), nil},
		{"inch mm 0.039", `0.039" (1.00mm)`, "numeric", pf(1.00e-3), nil},
		{"inch mm 0.047", `0.047" (1.20mm)`, "numeric", pf(1.20e-3), nil},
		{"inch mm 0.171", `0.171" (4.35mm)`, "numeric", pf(4.35e-3), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.text, tt.valueType)
			if tt.wantVal == nil {
				if got.Value != nil {
					t.Errorf("Value: got %v, want nil", *got.Value)
				}
			} else {
				if got.Value == nil {
					t.Errorf("Value: got nil, want %v", *tt.wantVal)
				} else if !approx(*got.Value, *tt.wantVal) {
					t.Errorf("Value: got %v, want %v", *got.Value, *tt.wantVal)
				}
			}
			if tt.wantMax == nil {
				if got.ValueMax != nil {
					t.Errorf("ValueMax: got %v, want nil", *got.ValueMax)
				}
			} else {
				if got.ValueMax == nil {
					t.Errorf("ValueMax: got nil, want %v", *tt.wantMax)
				} else if !approx(*got.ValueMax, *tt.wantMax) {
					t.Errorf("ValueMax: got %v, want %v", *got.ValueMax, *tt.wantMax)
				}
			}
		})
	}
}

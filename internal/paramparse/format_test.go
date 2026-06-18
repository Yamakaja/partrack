package paramparse

import "testing"

func TestFormatEngineering(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{10000, "Ω", "10 kΩ"},
		{4700, "Ω", "4.7 kΩ"},
		{1, "Ω", "1 Ω"},
		{100e-9, "F", "100 nF"},
		{2.2e-6, "H", "2.2 µH"},
		{1e6, "Ω", "1 MΩ"},
		{0, "Ω", "0 Ω"},
		{1, "%", "1 %"},
		{50, "V", "50 V"},
	}
	for _, c := range cases {
		if got := FormatEngineering(c.v, c.unit); got != c.want {
			t.Errorf("FormatEngineering(%g, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestFormatRKM(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{10000, "Ω", "10K"},
		{4700, "Ω", "4K7"},
		{1, "Ω", "1R"},
		{4.7, "Ω", "4R7"},
		{100, "Ω", "100R"},
		{100e-9, "F", "100N"},
		{2.2e-6, "H", "2U2"},
		{1e6, "Ω", "1M"},
		{1, "%", "1%"},
	}
	for _, c := range cases {
		if got := FormatRKM(c.v, c.unit); got != c.want {
			t.Errorf("FormatRKM(%g, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

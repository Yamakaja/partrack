package eseries

import (
	"math"
	"testing"
)

func TestByName(t *testing.T) {
	for _, name := range []string{"E6", "e12", " E24 ", "E96"} {
		if _, ok := ByName(name); !ok {
			t.Errorf("ByName(%q) not found", name)
		}
	}
	if _, ok := ByName("E5"); ok {
		t.Error("ByName(E5) should not be found")
	}
}

// TestGeometricKnownValues spot-checks the generated E96 series against the
// canonical IEC 60063 table (first several entries).
func TestGeometricKnownValues(t *testing.T) {
	want := []float64{1.00, 1.02, 1.05, 1.07, 1.10, 1.13, 1.15, 1.18, 1.21, 1.24}
	for i, w := range want {
		if got := E96.base[i]; math.Abs(got-w) > 1e-9 {
			t.Errorf("E96.base[%d] = %v, want %v", i, got, w)
		}
	}
	if len(E48.base) != 48 || len(E96.base) != 96 || len(E192.base) != 192 {
		t.Errorf("series lengths: E48=%d E96=%d E192=%d", len(E48.base), len(E96.base), len(E192.base))
	}
}

func TestExpand(t *testing.T) {
	eq := func(a, b []float64) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if math.Abs(a[i]-b[i]) > a[i]*1e-9+1e-15 {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name   string
		series Series
		lo, hi float64
		want   []float64
	}{
		{
			name: "E12 one decade", series: E12, lo: 1, hi: 10,
			want: []float64{1.0, 1.2, 1.5, 1.8, 2.2, 2.7, 3.3, 3.9, 4.7, 5.6, 6.8, 8.2, 10},
		},
		{
			name: "E24 10 to 100 includes both endpoints", series: E24, lo: 10, hi: 100,
			want: []float64{10, 11, 12, 13, 15, 16, 18, 20, 22, 24, 27, 30, 33, 36, 39, 43, 47, 51, 56, 62, 68, 75, 82, 91, 100},
		},
		{
			name: "E6 spanning decades", series: E6, lo: 100, hi: 10000,
			want: []float64{100, 150, 220, 330, 470, 680, 1000, 1500, 2200, 3300, 4700, 6800, 10000},
		},
		{
			name: "E12 sub-unit capacitance range", series: E12, lo: 1e-9, hi: 1e-8,
			want: []float64{1e-9, 1.2e-9, 1.5e-9, 1.8e-9, 2.2e-9, 2.7e-9, 3.3e-9, 3.9e-9, 4.7e-9, 5.6e-9, 6.8e-9, 8.2e-9, 1e-8},
		},
		{
			name: "off-grid bounds trim ends", series: E24, lo: 25, hi: 60,
			want: []float64{27, 30, 33, 36, 39, 43, 47, 51, 56},
		},
		{
			name: "single value range", series: E12, lo: 4700, hi: 4700,
			want: []float64{4700},
		},
		{name: "invalid range", series: E12, lo: 100, hi: 10, want: nil},
		{name: "zero lo", series: E12, lo: 0, hi: 10, want: nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.series.Expand(c.lo, c.hi)
			if !eq(got, c.want) {
				t.Errorf("Expand(%g,%g) = %v\nwant %v", c.lo, c.hi, got, c.want)
			}
		})
	}
}

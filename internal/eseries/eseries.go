// Package eseries generates IEC 60063 preferred-number ("E series") values across
// an arbitrary magnitude range. It is pure and DB-free so it can be unit-tested in
// isolation; the values it returns are plain base-magnitude floats in the value's
// natural unit (Ω, F, H, …), to be formatted and resolved by the caller.
package eseries

import (
	"math"
	"strings"
)

// Series is one preferred-number series: an ordered set of base values spanning a
// single decade [1, 10).
type Series struct {
	Name string
	base []float64
}

// The low series (E3–E24) predate the geometric definition and use the historical
// IEC 60063 tables verbatim — they deviate slightly from the n-th roots of 10, so
// they cannot be generated. E48 and above are exactly the rounded geometric series.
var (
	E3   = Series{"E3", []float64{1.0, 2.2, 4.7}}
	E6   = Series{"E6", []float64{1.0, 1.5, 2.2, 3.3, 4.7, 6.8}}
	E12  = Series{"E12", []float64{1.0, 1.2, 1.5, 1.8, 2.2, 2.7, 3.3, 3.9, 4.7, 5.6, 6.8, 8.2}}
	E24  = Series{"E24", []float64{1.0, 1.1, 1.2, 1.3, 1.5, 1.6, 1.8, 2.0, 2.2, 2.4, 2.7, 3.0, 3.3, 3.6, 3.9, 4.3, 4.7, 5.1, 5.6, 6.2, 6.8, 7.5, 8.2, 9.1}}
	E48  = Series{"E48", geometric(48)}
	E96  = Series{"E96", geometric(96)}
	E192 = Series{"E192", geometric(192)}
)

var byName = map[string]Series{
	"E3": E3, "E6": E6, "E12": E12, "E24": E24, "E48": E48, "E96": E96, "E192": E192,
}

// ByName looks up a series by its (case-insensitive) name, e.g. "E24".
func ByName(name string) (Series, bool) {
	s, ok := byName[strings.ToUpper(strings.TrimSpace(name))]
	return s, ok
}

// geometric builds one decade of an En series (n ∈ {48,96,192}) from the IEC 60063
// definition: the n-th roots of 10, each rounded to three significant figures.
func geometric(n int) []float64 {
	out := make([]float64, n)
	for k := range n {
		// 10^(k/n) lies in [1, 10); 3 significant figures ⇒ 2 decimal places.
		out[k] = math.Round(math.Pow(10, float64(k)/float64(n))*100) / 100
	}
	return out
}

// Expand returns every preferred value of the series within [lo, hi] (inclusive),
// across all decades, in ascending order. lo and hi are arbitrary positive
// magnitudes in the value's natural unit. It returns nil for an empty or invalid
// range. The number of decades is bounded by the caller's range, but Expand itself
// imposes no count limit — callers should cap the result for batch operations.
func (s Series) Expand(lo, hi float64) []float64 {
	if lo <= 0 || hi <= 0 || hi < lo || len(s.base) == 0 {
		return nil
	}
	const eps = 1e-9
	var out []float64
	// Start a decade below floor(log10(lo)) to absorb floating-point boundary error,
	// then walk decades upward until even the smallest value exceeds hi.
	startD := int(math.Floor(math.Log10(lo))) - 1
	for d := startD; math.Pow(10, float64(d)) <= hi*(1+eps); d++ {
		decade := math.Pow(10, float64(d))
		for _, b := range s.base {
			v := clean(b * decade)
			if v < lo*(1-eps) {
				continue
			}
			if v > hi*(1+eps) {
				return out // base values ascend within and across decades — done
			}
			out = append(out, v)
		}
	}
	return out
}

// clean damps floating-point noise from base×decade (e.g. 4.7×1000 = 4700.0000001)
// by rounding to ~10 significant figures, well beyond any real component precision.
func clean(v float64) float64 {
	if v == 0 {
		return 0
	}
	mag := math.Pow(10, math.Floor(math.Log10(math.Abs(v))))
	return math.Round(v/mag*1e9) / 1e9 * mag
}

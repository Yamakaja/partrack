// Package paramparse parses distributor parameter value text into structured
// numeric values normalized to base SI units.
package paramparse

import "strings"

// siPrefix maps a prefix symbol to its multiplier.
var siPrefixes = []struct {
	symbol     string
	multiplier float64
}{
	{"T", 1e12},
	{"G", 1e9},
	{"M", 1e6},
	{"k", 1e3},
	{"K", 1e3},
	// base (no prefix) handled implicitly
	{"m", 1e-3},
	{"u", 1e-6},
	{"µ", 1e-6},
	{"n", 1e-9},
	{"p", 1e-12},
}

// prefixMultiplier returns the SI multiplier for the given prefix symbol,
// or 1.0 if the symbol is not a recognised SI prefix.
func prefixMultiplier(prefix string) float64 {
	for _, p := range siPrefixes {
		if p.symbol == prefix {
			return p.multiplier
		}
	}
	return 1.0
}

// storageMultiplier returns the bit-equivalent multiplier for a storage prefix+unit pair.
// Byte units (B, byte, bytes) multiply by 8; bit units do not.
// Binary IEC prefixes (Ki/Mi/Gi/Ti) use powers of 1024; SI prefixes use powers of 10.
func storageMultiplier(prefix, unit string) float64 {
	var mult float64
	switch prefix {
	case "Ki":
		mult = 1 << 10
	case "Mi":
		mult = 1 << 20
	case "Gi":
		mult = 1 << 30
	case "Ti":
		mult = float64(int64(1) << 40)
	case "k", "K":
		mult = 1e3
	case "M":
		mult = 1e6
	case "G":
		mult = 1e9
	case "T":
		mult = 1e12
	default:
		mult = 1
	}
	lower := strings.ToLower(unit)
	if lower == "b" || strings.HasPrefix(lower, "byte") {
		mult *= 8
	}
	return mult
}

// normalizeText replaces common Unicode characters with ASCII equivalents
// to simplify subsequent parsing.
func normalizeText(s string) string {
	r := strings.NewReplacer(
		"\u00b5", "u", // µ → u (MICRO SIGN)
		"\u03bc", "u", // μ → u (GREEK SMALL LETTER MU)
		"\u03a9", "Ohm", // Ω → Ohm
		"\u2126", "Ohm", // Ω → Ohm (OHM SIGN)
		"\u00b1", "±", // ± kept as-is (ASCII ±)
		"\u00b0", "°", // ° kept as-is
		"\u2013", "~", // – → ~
		"\u2014", "~", // — → ~
		" to ", " ~ ",
		"...", " ~ ",
	)
	return r.Replace(s)
}

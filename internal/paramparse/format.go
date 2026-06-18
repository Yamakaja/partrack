package paramparse

import (
	"math"
	"strconv"
	"strings"
)

// roundSig damps floating-point noise (e.g. 100e-9/1e-9 = 100.00000000000001) so a
// value that should read as a round number does. 6 decimal places on the scaled
// mantissa is well within parameter precision.
func roundSig(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func trimFloat(v float64) string { return strconv.FormatFloat(roundSig(v), 'f', -1, 64) }

var engPrefixes = []struct {
	factor float64
	sym    string
}{
	{1e9, "G"}, {1e6, "M"}, {1e3, "k"}, {1, ""},
	{1e-3, "m"}, {1e-6, "µ"}, {1e-9, "n"}, {1e-12, "p"},
}

// FormatEngineering renders a base-SI value with an SI prefix and the unit symbol,
// e.g. (10000,"Ω")→"10 kΩ", (100e-9,"F")→"100 nF", (1,"Ω")→"1 Ω". Without a unit it
// returns the bare number; "%" and ratio units (containing "/") are not SI-scaled.
func FormatEngineering(v float64, unitSymbol string) string {
	if unitSymbol == "" {
		return trimFloat(v)
	}
	if unitSymbol == "%" || strings.Contains(unitSymbol, "/") {
		return trimFloat(v) + " " + unitSymbol
	}
	if v == 0 {
		return "0 " + unitSymbol
	}
	a := math.Abs(v)
	for _, p := range engPrefixes {
		if a >= p.factor || p.factor == 1e-12 {
			return trimFloat(v/p.factor) + " " + p.sym + unitSymbol
		}
	}
	return trimFloat(v) + " " + unitSymbol
}

var rkmPrefixes = []struct {
	factor float64
	letter string
}{
	{1e9, "G"}, {1e6, "M"}, {1e3, "K"}, {1, ""},
	{1e-3, "m"}, {1e-6, "U"}, {1e-9, "N"}, {1e-12, "P"},
}

// FormatRKM renders a base-SI value in compact RKM / IEC-60062 notation for use in
// identifiers: the SI-prefix letter stands in for the decimal point and the unit is
// implied by context, e.g. (4700,"Ω")→"4K7", (10000,"Ω")→"10K", (1,"Ω")→"1R",
// (100e-9,"F")→"100N", (2.2e-6,"H")→"2U2". "%" values pass through as "<n>%".
func FormatRKM(v float64, unitSymbol string) string {
	if unitSymbol == "%" {
		return trimFloat(v) + "%"
	}
	if v == 0 {
		return "0"
	}
	a := math.Abs(v)
	chosen := rkmPrefixes[len(rkmPrefixes)-1]
	for _, p := range rkmPrefixes {
		if a >= p.factor || p.factor == 1e-12 {
			chosen = p
			break
		}
	}
	letter := chosen.letter
	if letter == "" {
		letter = rkmBaseLetter(unitSymbol)
	}
	s := trimFloat(v / chosen.factor)
	if whole, frac, ok := strings.Cut(s, "."); ok {
		return whole + letter + frac
	}
	return s + letter
}

// rkmBaseLetter is the marker used when the value needs no SI prefix. Ohms use the
// classic "R"; other units fall back to the (uppercased) symbol.
func rkmBaseLetter(unitSymbol string) string {
	switch unitSymbol {
	case "Ω", "":
		return "R"
	default:
		return strings.ToUpper(unitSymbol)
	}
}

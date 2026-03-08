package paramparse

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ParseResult holds the outcome of parsing one parameter value string.
// Value is nil when the text cannot be parsed as a number (non-numeric types,
// unparseable text). ValueMax is non-nil only for range parameters.
type ParseResult struct {
	Value    *float64
	ValueMax *float64
}

// numericRe matches an optional sign, digits (with optional decimal point),
// an optional SI prefix character, and an optional unit suffix.
// Groups: 1=sign+digits, 2=prefix, 3=unit-text (may be empty).
var numericRe = regexp.MustCompile(
	`^\s*([+-]?[0-9]+(?:\.[0-9]*)?(?:[eE][+-]?[0-9]+)?)\s*` +
		`([TGMkmKunpµ]?)` +
		`\s*([A-Za-zΩ°%/]*)\s*$`,
)

// toleranceRe matches tolerance strings like "±1%", "± 0.5 %", "+/-5%".
var toleranceRe = regexp.MustCompile(
	`^[±+\-/]*\s*([0-9]+(?:\.[0-9]*)?)\s*%\s*$`,
)

// ppmToleranceRe matches ppm tolerance strings like "±30ppm", "30 ppm".
// The captured value must be converted to percent (× 1e-4) before storing.
var ppmToleranceRe = regexp.MustCompile(
	`^[±+\-/]*\s*([0-9]+(?:\.[0-9]*)?)\s*ppm\s*$`,
)

// ppbToleranceRe matches ppb tolerance strings like "±500ppb", "500 ppb".
// The captured value must be converted to percent (× 1e-7) before storing.
var ppbToleranceRe = regexp.MustCompile(
	`^[±+\-/]*\s*([0-9]+(?:\.[0-9]*)?)\s*ppb\s*$`,
)

// rangeRe matches "X ~ Y" range patterns (after normalizeText substitution).
var rangeRe = regexp.MustCompile(
	`^(.+?)\s*~\s*(.+)$`,
)

// inchMmRe matches dual-unit patterns like `0.015" (0.37mm)` and captures
// the metric millimetre value so it can be used in place of the full string.
var inchMmRe = regexp.MustCompile(
	`^\s*[0-9]+(?:\.[0-9]*)?\"\s*\(([0-9]+(?:\.[0-9]*)?)mm\)\s*$`,
)

// trailingParenRe matches a trailing parenthetical annotation like " (x2 Channels)".
var trailingParenRe = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

// storageSizeRe matches storage size values with SI or IEC binary prefixes.
// Binary prefixes (Ki/Mi/Gi/Ti) must appear before single-char SI prefixes in the
// alternation so they are tried first. Groups: 1=number, 2=prefix, 3=unit.
var storageSizeRe = regexp.MustCompile(
	`^\s*([+-]?[0-9]+(?:\.[0-9]*)?(?:[eE][+-]?[0-9]+)?)\s*` +
		`(Ki|Mi|Gi|Ti|[kKMGT])?(bits?|Bits?|bytes?|Bytes?|B)\s*$`,
)

// Parse extracts numeric value(s) from distributor parameter text.
// valueType should be the partrack value_type ('numeric', 'range', 'text', 'enum').
// Returns a zero-value ParseResult (both nil) for non-numeric types or unparseable text.
func Parse(text string, valueType string) ParseResult {
	if valueType == "text" || valueType == "enum" {
		return ParseResult{}
	}

	text = strings.TrimSpace(text)
	if text == "" || text == "-" || text == "N/A" || text == "n/a" {
		return ParseResult{}
	}

	text = normalizeText(text)

	// Dual-unit inch+mm: `0.015" (0.37mm)` → `0.37mm`
	if m := inchMmRe.FindStringSubmatch(text); m != nil {
		text = m[1] + "mm"
	} else {
		// Strip trailing parenthetical annotations: `800µA (x2 Channels)` → `800µA`
		text = trailingParenRe.ReplaceAllString(text, "")
	}

	// Tolerance: "±1%" → 1.0
	if m := toleranceRe.FindStringSubmatch(text); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			return ParseResult{Value: &v}
		}
	}

	// PPM tolerance: "±30ppm" → 0.003 (converted to %-equivalent: ppm × 1e-4)
	if m := ppmToleranceRe.FindStringSubmatch(text); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			v *= 1e-4
			return ParseResult{Value: &v}
		}
	}

	// PPB tolerance: "±500ppb" → 5e-5 (converted to %-equivalent: ppb × 1e-7)
	if m := ppbToleranceRe.FindStringSubmatch(text); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			v *= 1e-7
			return ParseResult{Value: &v}
		}
	}

	// Range: "X ~ Y"
	if m := rangeRe.FindStringSubmatch(text); m != nil {
		lo := parseScalar(m[1])
		hi := parseScalar(m[2])
		if lo != nil && hi != nil {
			return ParseResult{Value: lo, ValueMax: hi}
		}
		if lo != nil {
			return ParseResult{Value: lo}
		}
	}

	// Single scalar value
	v := parseScalar(text)
	if v != nil {
		return ParseResult{Value: v}
	}

	return ParseResult{}
}

// parseScalar attempts to parse a single numeric value (with optional SI prefix)
// from s. Returns nil if the string is not parseable.
func parseScalar(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return nil
	}

	// Strip leading ± or + for tolerances / one-sided ranges
	s = strings.TrimLeft(s, "±+ ")

	// Fast path for purely numeric strings (no unit)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return &v
	}

	// Storage size: "9 Mbit", "1 KiB", "256 GiB" → value in bits
	if m := storageSizeRe.FindStringSubmatch(s); m != nil {
		base, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			v := base * storageMultiplier(m[2], m[3])
			return &v
		}
	}

	m := numericRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	numStr := m[1]
	prefix := m[2]

	// Sanity-check: the "unit" part should not look like digits.
	// If the entire string is digits+letters with no recognisable SI prefix,
	// check if it's a package code like "0402" — no prefix means prefix="".
	if prefix == "" && m[3] != "" {
		// There is a unit suffix but no SI prefix.
		// Allow known alphanumeric unit suffixes (Ohm, Hz, V, A, W, F, H, etc.)
		// Reject if the "unit" part is purely digits (e.g. "0402").
		if isAllDigits(m[3]) {
			return nil
		}
	}

	base, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return nil
	}

	result := base * prefixMultiplier(prefix)
	return &result
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

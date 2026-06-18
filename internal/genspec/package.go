// Package genspec parses generic/parametric component specifications — reference
// designators, EDA footprint strings, and value fields — into a structured
// interpretation that can be matched against, or used to create, partrack Parts.
//
// It is a best-effort token scanner, not a strict grammar: it extracts whatever
// it can and leaves the rest empty for human review. It sits above paramparse
// (which it uses for SI-value parsing) and is deliberately free of any database
// dependency so it can be unit-tested in isolation. See doc/bom-import.md.
package genspec

import (
	"regexp"
	"strings"
)

// metricToImperial maps a metric chip-size code to its imperial code. Note that
// the ambiguity cascades: "0402" and "0603" appear as both imperial codes and as
// the metric twins of "01005"/"0201" — see CanonicalPackage for how the metric
// twin is disambiguated structurally.
var metricToImperial = map[string]string{
	"0201": "008004",
	"0402": "01005",
	"0603": "0201",
	"1005": "0402",
	"1608": "0603",
	"2012": "0805",
	"2520": "1008",
	"3216": "1206",
	"3225": "1210",
	"3246": "1218",
	"4532": "1812",
	"5025": "2010",
	"6332": "2512",
	"7142": "2816",
}

// imperialSizes is the set of recognised imperial chip-size codes. Codes are
// variable length: down to 008004 (6 digits) and 01005 (5 digits).
var imperialSizes = map[string]bool{
	"008004": true, "01005": true, "0201": true, "0402": true, "0603": true,
	"0805": true, "1008": true, "1206": true, "1210": true, "1218": true,
	"1812": true, "2010": true, "2512": true, "2816": true, "2920": true,
}

// footprintClassToken maps the leading token of a KiCad-style footprint to a
// component class key (see classify.go).
var footprintClassToken = map[string]string{
	"R": "R", "C": "C", "L": "L", "D": "D", "LED": "LED",
	"FB": "FB", "Q": "Q", "U": "U", "Y": "Y", "F": "F",
}

var (
	// structMetricRe matches the KiCad <imperial>_<metric>Metric structure; the
	// imperial code is the token immediately before _<NNNN>Metric. Both groups are
	// digit runs so "WSK2512_6332Metric" captures imperial 2512.
	structMetricRe = regexp.MustCompile(`(\d{3,6})_(\d{3,4})Metric`)
	// parenMetricRe matches the distributor "<imperial> (<metric> Metric)" form,
	// e.g. "1008 (2520 Metric)" — the imperial code is the leading group.
	parenMetricRe = regexp.MustCompile(`(\d{3,6})\s*\(\s*(\d{3,4})\s*Metric\s*\)`)
	// metricOnlyRe matches a standalone <NNNN>Metric (no preceding imperial token).
	metricOnlyRe = regexp.MustCompile(`(\d{3,4})Metric`)
	// ipcRe matches IPC-7351 names like RESC1608X55N / CAPC2012X127N — a family
	// prefix followed by a 4-digit metric code.
	ipcRe = regexp.MustCompile(`(?i)^(?:RES|CAP|IND|LED|DIO|FUS|FER)C?(\d{4})`)
	// bareImperialRe matches a standalone imperial-looking code (no Metric sibling).
	bareCodeRe = regexp.MustCompile(`\d{4,6}`)
	// dimensionTokenRe matches footprint noise tokens carrying physical dimensions
	// (pad sizes, lead lengths, pitches), used to trim family names.
	dimensionTokenRe = regexp.MustCompile(`(?i)^(Pad|HandSolder|L\d|D\d|W\d|P\d|\d+(\.\d+)?x?\d*(\.\d+)?mm).*`)
)

// NormalizePackage returns the canonical package identifier for a raw
// package/footprint string, or "" when none can be determined. It is the
// convenience wrapper used when only the canonical value is needed (e.g. to
// populate parts.canonical_package on both manual creation and enrichment).
func NormalizePackage(s string) string {
	c, _, _, ok := CanonicalPackage(s)
	if !ok {
		return ""
	}
	return c
}

// NormalizeKey reduces a raw value or footprint string to a stable lookup key
// for the interpretation memory: trimmed, internal whitespace collapsed, and
// lowercased. This lets "10k", "10K" and "10 k" all hit the same remembered
// interpretation. It is intentionally lossy — the key only needs to be stable,
// the stored interpretation carries the real meaning.
func NormalizeKey(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// CanonicalPackage normalises an EDA footprint/package string into a canonical
// package identifier, a mounting type ("smd" | "tht" | ""), and a class hint
// (a class key, or "" if none). ok is false when no package could be confidently
// determined (e.g. resistor arrays). The raw input should be retained separately
// by the caller for traceability.
func CanonicalPackage(footprint string) (canonical, mounting, classHint string, ok bool) {
	s := strings.TrimSpace(footprint)
	if s == "" {
		return "", "", "", false
	}

	// Strip a "library:footprint" qualifier.
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}

	tokens := strings.Split(s, "_")
	// Class hint from the leading token (R_, C_, LED_, ...).
	if len(tokens) > 0 {
		if c, found := footprintClassToken[tokens[0]]; found {
			classHint = c
		}
	}

	mounting = detectMounting(s)

	// Resistor/connector arrays have no single 2-pad chip size — don't guess the
	// size, but they are SMD.
	low := strings.ToLower(s)
	if strings.Contains(low, "array") || strings.Contains(low, "cat16") ||
		strings.Contains(low, "convex") || strings.Contains(low, "concave") {
		if mounting == "" {
			mounting = "smd"
		}
		return "", mounting, classHint, false
	}

	if size, found := chipSize(s); found {
		if mounting == "" {
			mounting = "smd"
		}
		return size, mounting, classHint, true
	}

	// Not a chip size: produce a cleaned family name (SOT-23, SOIC-8, Axial DIN0207).
	if fam := familyName(tokens, classHint); fam != "" {
		if mounting == "" {
			mounting = "smd"
		}
		return fam, mounting, classHint, true
	}

	return "", mounting, classHint, false
}

// chipSize extracts an imperial chip-size code from a footprint string.
func chipSize(s string) (string, bool) {
	// 1. Structural <imperial>_<metric>Metric — the imperial token wins; the metric
	//    twin (e.g. 0603 in R_0201_0603Metric) is deliberately ignored.
	if m := structMetricRe.FindStringSubmatch(s); m != nil {
		imp := m[1]
		if imperialSizes[imp] {
			return imp, true
		}
		// Fall back to mapping the metric code if the imperial token is unfamiliar.
		if mapped, found := metricToImperial[m[2]]; found {
			return mapped, true
		}
		return imp, true
	}
	// 1b. Distributor "<imperial> (<metric> Metric)" form — same imperial-wins rule.
	if m := parenMetricRe.FindStringSubmatch(s); m != nil {
		imp := m[1]
		if imperialSizes[imp] {
			return imp, true
		}
		if mapped, found := metricToImperial[m[2]]; found {
			return mapped, true
		}
		return imp, true
	}
	// 2. Standalone <metric>Metric.
	if m := metricOnlyRe.FindStringSubmatch(s); m != nil {
		if mapped, found := metricToImperial[m[1]]; found {
			return mapped, true
		}
	}
	// 3. IPC-7351 family name carrying a metric code.
	if m := ipcRe.FindStringSubmatch(s); m != nil {
		if mapped, found := metricToImperial[m[1]]; found {
			return mapped, true
		}
	}
	// 4. Bare code with no Metric sibling → imperial reading when in the known set.
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == ' ' }) {
		if c := bareCodeRe.FindString(tok); c != "" && imperialSizes[c] {
			return c, true
		}
	}
	return "", false
}

// detectMounting infers SMD vs through-hole from textual cues. Returns "" when
// undetermined (the caller defaults chip/family packages to smd).
func detectMounting(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "axial"), strings.Contains(low, "radial"),
		strings.Contains(low, "tht"), strings.Contains(low, "through"),
		strings.Contains(low, "_dip"), strings.HasPrefix(low, "dip"):
		return "tht"
	case strings.Contains(low, "handsolder"), strings.Contains(low, "smd"):
		return "smd"
	}
	return ""
}

// familyName cleans a non-chip footprint down to a package family identifier
// (SOT-23, SOIC-8, Axial DIN0207). Returns "" if nothing meaningful remains.
func familyName(tokens []string, classHint string) string {
	out := make([]string, 0, len(tokens))
	for i, t := range tokens {
		if t == "" {
			continue
		}
		// Drop a leading class-prefix token (R_, C_, LED_).
		if i == 0 && classHint != "" && footprintClassToken[t] == classHint {
			continue
		}
		// Stop at the first dimension/noise token — the family name precedes it.
		if dimensionTokenRe.MatchString(t) {
			break
		}
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

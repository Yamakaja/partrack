// Package mfrmatch resolves manufacturer-name spellings to a canonical identity so
// the same manufacturer named differently across distributors ("Texas Instruments"
// vs "TI") is treated as one. It has two layers:
//
//   - Normalize: a conservative, always-on string normalization (the floor). It
//     strips punctuation and trailing legal suffixes but never descriptive words, so
//     distinct manufacturers stay distinct.
//   - Resolver: an in-memory alias table (seeded + admin-curated) layered on top of
//     Normalize. It only ever *adds* equivalences; with an empty table the resolver
//     reproduces Normalize-only behaviour exactly.
//
// Match and dedup call sites (worker enrichByMPN / ApplyEnrichmentResult, scan and
// resolve lookups) consult a single shared Resolver.
package mfrmatch

import "strings"

// corporateSuffixes are trailing legal-form tokens stripped before comparing
// manufacturer names, so "Texas Instruments" and "Texas Instruments Inc" match.
var corporateSuffixes = map[string]bool{
	"inc": true, "incorporated": true, "corp": true, "corporation": true,
	"co": true, "company": true, "ltd": true, "limited": true, "llc": true,
	"gmbh": true, "ag": true, "sa": true, "srl": true, "plc": true, "nv": true,
	"bv": true, "pte": true, "kg": true, "ab": true, "oy": true,
}

// Normalize lowercases, replaces every non-alphanumeric rune with a space, collapses
// whitespace, and drops trailing corporate-form tokens (Inc, Ltd, …). It is
// intentionally conservative: it does not strip descriptive words like
// "Technology"/"Semiconductor", so distinct manufacturers stay distinct. This is the
// always-on floor beneath the alias table.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	fields := strings.Fields(b.String())
	for len(fields) > 1 && corporateSuffixes[fields[len(fields)-1]] {
		fields = fields[:len(fields)-1]
	}
	return strings.Join(fields, " ")
}

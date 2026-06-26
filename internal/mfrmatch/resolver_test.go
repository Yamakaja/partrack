package mfrmatch

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Texas Instruments":                      "texas instruments",
		"Texas Instruments Inc.":                 "texas instruments",
		"Texas Instruments, Inc":                 "texas instruments",
		"STMicroelectronics":                     "stmicroelectronics",
		"Lumimax Optoelectronic Technology":      "lumimax optoelectronic technology",
		"Jiangsu Changjing Electronics Co., Ltd": "jiangsu changjing electronics",
		"TI":                                     "ti",
		"":                                       "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// tiAliases maps both "Texas Instruments" and "TI" to one canonical identity.
func tiAliases() []Alias {
	return []Alias{
		{AliasNormalized: "texas instruments", ManufacturerID: "m1", CanonicalName: "Texas Instruments", NormalizedName: "texas instruments"},
		{AliasNormalized: "ti", ManufacturerID: "m1", CanonicalName: "Texas Instruments", NormalizedName: "texas instruments"},
	}
}

func TestResolverMatches_Aliased(t *testing.T) {
	r := NewStaticResolver(tiAliases())

	// Alias-aware equivalences across spellings.
	if !r.Matches("TI", "Texas Instruments") {
		t.Error("TI should match Texas Instruments via alias")
	}
	if !r.Matches("Texas Instruments Inc.", "ti") {
		t.Error("normalization + alias should match TI to Texas Instruments Inc.")
	}
	// Distinct manufacturers stay distinct.
	if r.Matches("TI", "STMicroelectronics") {
		t.Error("TI must not match an unrelated manufacturer")
	}
}

func TestResolverMatches_EmptyTargetAndUnknown(t *testing.T) {
	r := NewStaticResolver(nil) // empty table → Normalize-only behaviour

	// Empty want matches anything (manufacturer unknown → MPN-only fallback).
	if !r.Matches("Texas Instruments", "") {
		t.Error("empty want should match anything")
	}
	// A non-empty want never matches an empty/unknown candidate.
	if r.Matches("", "Texas Instruments") {
		t.Error("empty got should not match a non-empty want")
	}
	// With no aliases, equivalence falls back to conservative normalization.
	if !r.Matches("Texas Instruments", "Texas Instruments Inc") {
		t.Error("normalization floor should still match legal-suffix variants")
	}
	if r.Matches("TI", "Texas Instruments") {
		t.Error("without an alias table TI must not match Texas Instruments")
	}
}

func TestResolverCanonicalName(t *testing.T) {
	r := NewStaticResolver(tiAliases())

	if got, ok := r.CanonicalName("TI"); !ok || got != "Texas Instruments" {
		t.Errorf("CanonicalName(TI) = %q, %v; want Texas Instruments, true", got, ok)
	}
	// Unknown spelling is returned unchanged and flagged not-found.
	if got, ok := r.CanonicalName("Acme Widgets"); ok || got != "Acme Widgets" {
		t.Errorf("CanonicalName(Acme Widgets) = %q, %v; want Acme Widgets, false", got, ok)
	}
}

package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Yamakaja/partrack/internal/distributor"
)

func TestNormalizeManufacturer(t *testing.T) {
	cases := map[string]string{
		"Texas Instruments":                      "texas instruments",
		"Texas Instruments Inc.":                 "texas instruments",
		"Texas Instruments, Inc":                 "texas instruments",
		"STMicroelectronics":                     "stmicroelectronics",
		"Lumimax Optoelectronic Technology":      "lumimax optoelectronic technology",
		"Jiangsu Changjing Electronics Co., Ltd": "jiangsu changjing electronics",
		"":                                       "",
	}
	for in, want := range cases {
		if got := normalizeManufacturer(in); got != want {
			t.Errorf("normalizeManufacturer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestManufacturerMatches(t *testing.T) {
	cases := []struct {
		got, want string
		match     bool
	}{
		{"Texas Instruments", "Texas Instruments Inc", true},
		{"Texas Instruments", "Lumimax Optoelectronic Technology", false},
		// Empty target ("manufacturer unknown") matches anything so callers fall
		// back to MPN-only behaviour rather than rejecting everything.
		{"Texas Instruments", "", true},
		// A non-empty target never matches an empty/unknown candidate manufacturer.
		{"", "Texas Instruments", false},
	}
	for _, c := range cases {
		if got := manufacturerMatches(c.got, c.want); got != c.match {
			t.Errorf("manufacturerMatches(%q, %q) = %v, want %v", c.got, c.want, got, c.match)
		}
	}
}

// fakeEnrichClient is a minimal distributor.Client whose SearchParts/EnrichPart
// return canned data, for exercising enrichByMPN's candidate filtering.
type fakeEnrichClient struct {
	search    *distributor.SearchResult
	searchErr error
	enrich    map[string]*distributor.EnrichmentResult
	enriched  []string // records SKUs passed to EnrichPart
}

func (f *fakeEnrichClient) Name() string                                  { return "Fake" }
func (f *fakeEnrichClient) CredentialSpec() []distributor.CredentialField { return nil }
func (f *fakeEnrichClient) SearchParts(context.Context, distributor.SearchQuery, json.RawMessage) (*distributor.SearchResult, error) {
	return f.search, f.searchErr
}
func (f *fakeEnrichClient) EnrichPart(_ context.Context, sku string, _ json.RawMessage) (*distributor.EnrichmentResult, error) {
	f.enriched = append(f.enriched, sku)
	return f.enrich[sku], nil
}
func (f *fakeEnrichClient) RefreshListing(context.Context, string, json.RawMessage) ([]distributor.Listing, error) {
	return nil, nil
}

func newLM317SearchResult() *distributor.SearchResult {
	return &distributor.SearchResult{
		Products: []distributor.SearchProduct{
			{MPN: "LM317LCD", Manufacturer: "Texas Instruments", PrimarySKU: "TI-LCD"},                      // top hit, wrong MPN
			{MPN: "LM317", Manufacturer: "Texas Instruments", PrimarySKU: "TI-LM317"},                       // exact MPN, wrong mfr
			{MPN: "LM317", Manufacturer: "Lumimax Optoelectronic Technology Inc", PrimarySKU: "LUMI-LM317"}, // the real one
		},
	}
}

func enrichMapForLM317() map[string]*distributor.EnrichmentResult {
	return map[string]*distributor.EnrichmentResult{
		"TI-LCD":     {MPN: "LM317LCD", Manufacturer: "Texas Instruments"},
		"TI-LM317":   {MPN: "LM317", Manufacturer: "Texas Instruments"},
		"LUMI-LM317": {MPN: "LM317", Manufacturer: "Lumimax Optoelectronic Technology"},
	}
}

func TestEnrichByMPN_FiltersByManufacturer(t *testing.T) {
	f := &fakeEnrichClient{search: newLM317SearchResult(), enrich: enrichMapForLM317()}

	got, err := enrichByMPN(context.Background(), f, "LM317", "Lumimax Optoelectronic Technology", nil, 5)
	if err != nil {
		t.Fatalf("enrichByMPN: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 manufacturer-matched result, got %d", len(got))
	}
	if got[0].Manufacturer != "Lumimax Optoelectronic Technology" {
		t.Errorf("matched wrong manufacturer: %q", got[0].Manufacturer)
	}
	// The wrong-MPN top hit and wrong-manufacturer exact match must never be enriched.
	for _, sku := range f.enriched {
		if sku == "TI-LCD" || sku == "TI-LM317" {
			t.Errorf("enriched a non-matching candidate: %s", sku)
		}
	}
}

func TestEnrichByMPN_UnknownManufacturer_DropsTopResultFallback(t *testing.T) {
	f := &fakeEnrichClient{search: newLM317SearchResult(), enrich: enrichMapForLM317()}

	// Manufacturer unknown: keep every exact-MPN match, but never the unrelated
	// top result (LM317LCD) that the old best-effort fallback used to surface.
	got, err := enrichByMPN(context.Background(), f, "LM317", "", nil, 5)
	if err != nil {
		t.Fatalf("enrichByMPN: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 exact-MPN results, got %d", len(got))
	}
	for _, sku := range f.enriched {
		if sku == "TI-LCD" {
			t.Errorf("top-result fallback leaked an unrelated MPN: %s", sku)
		}
	}
}

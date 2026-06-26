package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/mfrmatch"
)

// Normalize/Matches unit tests live in internal/mfrmatch; these exercise how
// enrichByMPN applies the resolver to filter candidates.

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

// noAliases is a resolver with an empty alias table — equivalence falls back to
// conservative Normalize-only matching, the pre-resolver behaviour.
var noAliases = mfrmatch.NewStaticResolver(nil)

func TestEnrichByMPN_FiltersByManufacturer(t *testing.T) {
	f := &fakeEnrichClient{search: newLM317SearchResult(), enrich: enrichMapForLM317()}

	got, err := enrichByMPN(context.Background(), f, "LM317", "Lumimax Optoelectronic Technology", nil, 5, noAliases)
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
	got, err := enrichByMPN(context.Background(), f, "LM317", "", nil, 5, noAliases)
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

// With an alias table, a candidate spelled differently from the target ("TI" vs
// "Texas Instruments") is recognised as the same manufacturer and kept — the
// cross-distributor listing that plain normalization used to drop.
func TestEnrichByMPN_AliasMatchesAcrossSpellings(t *testing.T) {
	search := &distributor.SearchResult{
		Products: []distributor.SearchProduct{
			{MPN: "LM317", Manufacturer: "TI", PrimarySKU: "TI-LM317"},
		},
	}
	enrich := map[string]*distributor.EnrichmentResult{
		"TI-LM317": {MPN: "LM317", Manufacturer: "TI"},
	}
	f := &fakeEnrichClient{search: search, enrich: enrich}

	resolver := mfrmatch.NewStaticResolver([]mfrmatch.Alias{
		{AliasNormalized: "texas instruments", ManufacturerID: "m1", CanonicalName: "Texas Instruments", NormalizedName: "texas instruments"},
		{AliasNormalized: "ti", ManufacturerID: "m1", CanonicalName: "Texas Instruments", NormalizedName: "texas instruments"},
	})

	// Without the alias this would be dropped (normalize "ti" != "texas instruments").
	got, err := enrichByMPN(context.Background(), f, "LM317", "Texas Instruments", nil, 5, resolver)
	if err != nil {
		t.Fatalf("enrichByMPN: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the aliased candidate to be kept, got %d results", len(got))
	}
}

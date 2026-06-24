package digikey

import "testing"

// marketplaceLM317 mirrors the DigiKey productdetails response for 4491-LM317-ND:
// a single variation flagged MarketPlace (a third-party seller offer).
func marketplaceLM317() *product {
	return &product{
		ManufacturerPartNumber: "LM317",
		Manufacturer:           nameObj{Name: "Lumimax Optoelectronic Technology"},
		ProductUrl:             "https://www.digikey.com/en/products/detail/lumimax/LM317/29366241",
		ProductVariations: []productVariation{
			{
				DigiKeyProductNumber:            "4491-LM317-ND",
				MarketPlace:                     true,
				PackageType:                     nameObj{Name: "Bulk"},
				QuantityAvailableforPackageType: 4584,
				MinimumOrderQuantity:            1,
			},
		},
	}
}

func TestMapProductToEnrichment_IncludesRequestedMarketplaceSKU(t *testing.T) {
	r := mapProductToEnrichment(marketplaceLM317(), "4491-LM317-ND")
	if r.MPN != "LM317" || r.Manufacturer != "Lumimax Optoelectronic Technology" {
		t.Fatalf("unexpected metadata: mpn=%q mfr=%q", r.MPN, r.Manufacturer)
	}
	if len(r.Listings) != 1 {
		t.Fatalf("explicitly-requested marketplace SKU should yield 1 listing, got %d", len(r.Listings))
	}
	if r.Listings[0].SKU != "4491-LM317-ND" {
		t.Errorf("wrong listing SKU: %q", r.Listings[0].SKU)
	}
	if !r.Listings[0].Marketplace {
		t.Errorf("listing should be flagged Marketplace")
	}
}

func TestMapProductToEnrichment_SkipsUnrequestedMarketplaceSKU(t *testing.T) {
	// Same product reached via a different SKU (e.g. cross-distributor discovery):
	// the marketplace-only variation must stay excluded.
	r := mapProductToEnrichment(marketplaceLM317(), "some-other-sku")
	if len(r.Listings) != 0 {
		t.Fatalf("marketplace variation should be skipped when not explicitly requested, got %d listings", len(r.Listings))
	}
}

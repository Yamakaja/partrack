package worker

import "github.com/Yamakaja/partrack/internal/distributor"

// resolveEnrichmentResult is a lossless JSON projection of distributor.EnrichmentResult.
// Unlike serialisableEnrichment (used by the bulk-scan flow), this captures Parameters,
// Aliases, DistributorCategoryChain, and Listing.FixedOrderCost so that the commit path
// can call persistEnrichmentResult directly without making a second API call.
type resolveEnrichmentResult struct {
	MPN                      string                       `json:"mpn"`
	Manufacturer             string                       `json:"manufacturer"`
	Description              string                       `json:"description"`
	ShortDescription         string                       `json:"short_description"`
	Package                  string                       `json:"package"`
	DatasheetURL             string                       `json:"datasheet_url"`
	ProductURL               string                       `json:"product_url"`
	ImageURL                 string                       `json:"image_url"`
	LifecycleStatus          string                       `json:"lifecycle_status"`
	Aliases                  []string                     `json:"aliases,omitempty"`
	DistributorCategoryChain []resolveDistributorCategory `json:"distributor_category_chain,omitempty"`
	Parameters               []resolveParameter           `json:"parameters,omitempty"`
	Listings                 []resolveListing             `json:"listings"`
}

type resolveDistributorCategory struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type resolveParameter struct {
	DistributorParamID string `json:"distributor_param_id,omitempty"`
	Name               string `json:"name"`
	Value              string `json:"value"`
}

type resolveListing struct {
	SKU            string                   `json:"sku"`
	PackagingType  string                   `json:"packaging_type"`
	UnitPrice      string                   `json:"unit_price"`
	StockAvailable *int32                   `json:"stock_available"`
	Moq            int32                    `json:"moq"`
	FixedOrderCost string                   `json:"fixed_order_cost"`
	PriceBreaks    []distributor.PriceBreak `json:"price_breaks"`
	ListingURL     string                   `json:"listing_url,omitempty"`
}

// resolveEnrichmentEntry is one distributor's result stored in part_resolve_items.enrichment_data.
// Results holds the candidate products returned for this distributor; there is one entry per
// distinct product (e.g. chip vs eval board). SKU-based lookups always produce exactly one
// candidate; MPN-based searches may surface several exact matches.
type resolveEnrichmentEntry struct {
	DistributorID string                    `json:"distributor_id"`
	Name          string                    `json:"name"`
	Results       []*resolveEnrichmentResult `json:"results,omitempty"`
	Error         string                    `json:"error,omitempty"`
}

// resolveEnrichmentData is the top-level envelope stored in part_resolve_items.enrichment_data.
type resolveEnrichmentData struct {
	Distributors []resolveEnrichmentEntry `json:"distributors"`
}

// toResolveResult converts a distributor.EnrichmentResult to its lossless JSON form.
// Returns nil if r is nil.
func toResolveResult(r *distributor.EnrichmentResult) *resolveEnrichmentResult {
	if r == nil {
		return nil
	}
	out := &resolveEnrichmentResult{
		MPN:             r.MPN,
		Manufacturer:    r.Manufacturer,
		Description:     r.Description,
		ShortDescription: r.ShortDescription,
		Package:         r.Package,
		DatasheetURL:    r.DatasheetURL,
		ProductURL:      r.ProductURL,
		ImageURL:        r.ImageURL,
		LifecycleStatus: string(r.LifecycleStatus),
		Aliases:         r.Aliases,
	}
	for _, c := range r.DistributorCategoryChain {
		out.DistributorCategoryChain = append(out.DistributorCategoryChain, resolveDistributorCategory{
			Key:  c.Key,
			Name: c.Name,
		})
	}
	for _, p := range r.Parameters {
		out.Parameters = append(out.Parameters, resolveParameter{
			DistributorParamID: p.DistributorParamID,
			Name:               p.Name,
			Value:              p.Value,
		})
	}
	for _, l := range r.Listings {
		out.Listings = append(out.Listings, resolveListing{
			SKU:            l.SKU,
			PackagingType:  l.PackagingType,
			UnitPrice:      l.UnitPrice,
			StockAvailable: l.StockAvailable,
			Moq:            l.Moq,
			FixedOrderCost: l.FixedOrderCost,
			PriceBreaks:    l.PriceBreaks,
			ListingURL:     l.ListingURL,
		})
	}
	return out
}

// fromResolveResult reconstructs a distributor.EnrichmentResult from its stored JSON form.
// Used at commit time so persistEnrichmentResult receives a fully populated struct.
func fromResolveResult(r *resolveEnrichmentResult) *distributor.EnrichmentResult {
	if r == nil {
		return nil
	}
	out := &distributor.EnrichmentResult{
		MPN:             r.MPN,
		Manufacturer:    r.Manufacturer,
		Description:     r.Description,
		ShortDescription: r.ShortDescription,
		Package:         r.Package,
		DatasheetURL:    r.DatasheetURL,
		ProductURL:      r.ProductURL,
		ImageURL:        r.ImageURL,
		LifecycleStatus: distributor.LifecycleStatus(r.LifecycleStatus),
		Aliases:         r.Aliases,
	}
	for _, c := range r.DistributorCategoryChain {
		out.DistributorCategoryChain = append(out.DistributorCategoryChain, distributor.DistributorCategory{
			Key:  c.Key,
			Name: c.Name,
		})
	}
	for _, p := range r.Parameters {
		out.Parameters = append(out.Parameters, distributor.Parameter{
			DistributorParamID: p.DistributorParamID,
			Name:               p.Name,
			Value:              p.Value,
		})
	}
	for _, l := range r.Listings {
		out.Listings = append(out.Listings, distributor.Listing{
			SKU:            l.SKU,
			PackagingType:  l.PackagingType,
			UnitPrice:      l.UnitPrice,
			StockAvailable: l.StockAvailable,
			Moq:            l.Moq,
			FixedOrderCost: l.FixedOrderCost,
			PriceBreaks:    l.PriceBreaks,
			ListingURL:     l.ListingURL,
		})
	}
	return out
}

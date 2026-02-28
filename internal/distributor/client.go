// Package distributor defines the interface for distributor API integrations
// and the registry that maps api_type strings to Client implementations.
package distributor

import (
	"context"
	"encoding/json"
)

// Parameter is a name/value/unit triple returned by distributor enrichment.
type Parameter struct {
	Name  string
	Value string
	Unit  string // empty when unitless
}

// PriceBreak is a single quantity-price point from a distributor's price schedule.
type PriceBreak struct {
	BreakQty  int32
	UnitPrice string // decimal string e.g. "0.5000"
}

// Listing is full pricing/stock data for one distributor SKU.
type Listing struct {
	SKU            string
	PackagingType  string
	UnitPrice      string       // decimal string e.g. "0.5000"; convenience shorthand for PriceBreaks[0].UnitPrice; empty = unknown
	StockAvailable *int32       // nil = not yet known; 0 = out of stock
	Moq            int32
	FixedOrderCost string       // decimal string, default "0.0000"
	PriceBreaks    []PriceBreak // sorted ascending by BreakQty; may be empty
}

// SearchQuery describes a free-text distributor search request.
// Implementations should treat Keywords as the primary input and
// honour Limit/Offset according to their own documented bounds.
type SearchQuery struct {
	Keywords string
	Limit    int32
	Offset   int32
}

// SearchProduct is a minimal representation of a product returned
// by a distributor search. It is intentionally smaller than
// EnrichmentResult and omits parametric data.
type SearchProduct struct {
	MPN            string
	Manufacturer   string
	Description    string
	Category       string
	PrimarySKU     string
	Package        string
	UnitPrice      string
	StockAvailable *int32
}

// SearchResult wraps a single page of search results plus metadata.
type SearchResult struct {
	Products          []SearchProduct
	TotalCount        int32
	ExactMatchIndices []int
}

// EnrichmentResult is the combined metadata and listings returned by EnrichPart.
type EnrichmentResult struct {
	MPN             string // manufacturer part number; empty to leave unchanged
	Manufacturer    string
	Description     string
	Category        string
	Package         string
	DatasheetURL    string
	LifecycleStatus string // canonical DB enum value ("active", "nrnd", "obsolete", …) or "" to leave unchanged
	Parameters      []Parameter
	Listings        []Listing
}

// Client must be implemented by every distributor integration.
// credentials is the raw JSONB from distributor_configs.api_credentials;
// it is nil for distributors that require no authentication.
type Client interface {
	// EnrichPart fetches full product metadata and listings for a specific distributor SKU.
	// The SKU must be chosen unambiguously by the caller (e.g. selected from SearchParts results).
	// Returns nil, nil if the SKU is not found.
	EnrichPart(ctx context.Context, sku string, credentials json.RawMessage) (*EnrichmentResult, error)
	RefreshListing(ctx context.Context, sku string, credentials json.RawMessage) (*Listing, error)
	SearchParts(ctx context.Context, q SearchQuery, credentials json.RawMessage) (*SearchResult, error)
}

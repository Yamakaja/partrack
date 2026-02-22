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

// Listing is full pricing/stock data for one distributor SKU.
type Listing struct {
	SKU            string
	PackagingType  string
	UnitPrice      string // decimal string e.g. "0.5000"; empty = unknown
	StockAvailable *int32 // nil = not yet known; 0 = out of stock
	Moq            int32
	FixedOrderCost string // decimal string, default "0.0000"
}

// EnrichmentResult is the combined metadata and listings returned by EnrichPart.
type EnrichmentResult struct {
	Manufacturer string
	Description  string
	Category     string
	Package      string
	DatasheetURL string
	Parameters   []Parameter
	Listings     []Listing
}

// Client must be implemented by every distributor integration.
// credentials is the raw JSONB from distributor_configs.api_credentials;
// it is nil for distributors that require no authentication.
type Client interface {
	EnrichPart(ctx context.Context, mpn string, credentials json.RawMessage) (*EnrichmentResult, error)
	RefreshListing(ctx context.Context, sku string, credentials json.RawMessage) (*Listing, error)
}

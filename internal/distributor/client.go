// Package distributor defines the interface for distributor API integrations
// and the registry that maps api_type strings to Client implementations.
package distributor

import (
	"context"
	"encoding/json"
)

// LifecycleStatus represents the lifecycle state of a part as reported by a distributor.
// The zero value ("") means the distributor did not report a status and the existing
// DB value should be left unchanged.
type LifecycleStatus string

const (
	LifecycleStatusUnknown          LifecycleStatus = ""
	LifecycleStatusActive           LifecycleStatus = "active"
	LifecycleStatusNRND             LifecycleStatus = "nrnd"
	LifecycleStatusObsolete         LifecycleStatus = "obsolete"
	LifecycleStatusDeprecated       LifecycleStatus = "deprecated"
	LifecycleStatusNoLongerAvailable LifecycleStatus = "no_longer_available"
)

// Parameter is a name/value/unit triple returned by distributor enrichment.
type Parameter struct {
	Name  string
	Value string
	Unit  string // empty when unitless
}

// PriceBreak is a single quantity-price point from a distributor's price schedule.
type PriceBreak struct {
	BreakQty  int32  `json:"break_qty"`
	UnitPrice string `json:"unit_price"` // decimal string e.g. "0.5000"
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
	MPN              string // manufacturer part number; empty to leave unchanged
	Manufacturer     string
	Description      string          // long/detailed description
	ShortDescription string          // short/product description
	Category         string
	Package          string
	DatasheetURL     string
	ProductURL       string   // distributor product page URL
	ImageURL         string   // product photo URL
	Aliases          []string // alternative part numbers / other names
	LifecycleStatus  LifecycleStatus // "" (LifecycleStatusUnknown) means leave the existing DB value unchanged
	Parameters       []Parameter
	Listings         []Listing
}

// CredentialField describes one field in a distributor's credential form.
type CredentialField struct {
	Key         string `json:"key"`         // JSON key in stored credentials blob
	Label       string `json:"label"`       // Human-readable label shown in the UI
	Description string `json:"description"` // Help text displayed below the input
	Placeholder string `json:"placeholder"` // HTML placeholder attribute
	Secret      bool   `json:"secret"`      // Render as a password input when true
	Required    bool   `json:"required"`
	Type        string `json:"type"` // "string" (default) or "bool" (renders as checkbox)
}

// Client must be implemented by every distributor integration.
// credentials is the raw JSONB from distributor_configs.api_credentials;
// it is nil for distributors that require no authentication.
type Client interface {
	// CredentialSpec returns the list of fields required to configure this distributor.
	// The frontend uses this to render a typed form instead of a raw JSON textarea.
	// Return nil or an empty slice when no credentials are needed.
	CredentialSpec() []CredentialField

	// EnrichPart fetches full product metadata and listings for a specific distributor SKU.
	// The SKU must be chosen unambiguously by the caller (e.g. selected from SearchParts results).
	// Returns nil, nil if the SKU is not found.
	EnrichPart(ctx context.Context, sku string, credentials json.RawMessage) (*EnrichmentResult, error)
	RefreshListing(ctx context.Context, sku string, credentials json.RawMessage) (*Listing, error)
	SearchParts(ctx context.Context, q SearchQuery, credentials json.RawMessage) (*SearchResult, error)
}

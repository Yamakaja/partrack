// Package distributor defines the interface for distributor API integrations
// and the registry that maps api_type strings to Client implementations.
package distributor

import (
	"context"
	"encoding/json"
	"time"
)

// LifecycleStatus represents the lifecycle state of a part as reported by a distributor.
// The zero value ("") means the distributor did not report a status and the existing
// DB value should be left unchanged.
type LifecycleStatus string

const (
	LifecycleStatusUnknown           LifecycleStatus = ""
	LifecycleStatusActive            LifecycleStatus = "active"
	LifecycleStatusNRND              LifecycleStatus = "nrnd"
	LifecycleStatusObsolete          LifecycleStatus = "obsolete"
	LifecycleStatusDeprecated        LifecycleStatus = "deprecated"
	LifecycleStatusNoLongerAvailable LifecycleStatus = "no_longer_available"
)

// Parameter is a name/value/unit triple returned by distributor enrichment.
type Parameter struct {
	DistributorParamID string // distributor's own stable parameter ID (e.g. DigiKey's integer ID as string); empty if unavailable
	Name               string
	Value              string
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
	UnitPrice      string // decimal string e.g. "0.5000"; convenience shorthand for PriceBreaks[0].UnitPrice; empty = unknown
	StockAvailable *int32 // nil = not yet known; 0 = out of stock
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

// DistributorCategory is one level in a distributor's category hierarchy.
// Key is a stable, unique string the client guarantees is suitable as a DB
// lookup key in distributor_category_mappings.distributor_category_id.
// Clients with native category IDs should stringify them; clients without
// (e.g. Mouser) should use a stable, normalised form of the category name.
// Key must never be empty or "0" (reserved sentinel for "no category").
type DistributorCategory struct {
	Key  string
	Name string
}

// EnrichmentResult is the combined metadata and listings returned by EnrichPart.
type EnrichmentResult struct {
	MPN              string // manufacturer part number; empty to leave unchanged
	Manufacturer     string
	Description      string // long/detailed description
	ShortDescription string // short/product description
	// DistributorCategoryChain holds the full ancestry from most-specific (index 0)
	// to least-specific (last index). The client is solely responsible for providing
	// stable Key values; the worker uses them as-is for distributor_category_mappings
	// lookups. May be nil when no category information is available.
	DistributorCategoryChain []DistributorCategory
	Package                  string
	DatasheetURL             string
	ProductURL               string          // distributor product page URL
	ImageURL                 string          // product photo URL
	Aliases                  []string        // alternative part numbers / other names
	LifecycleStatus          LifecycleStatus // "" (LifecycleStatusUnknown) means leave the existing DB value unchanged
	Parameters               []Parameter
	Listings                 []Listing
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

// --- Ordering types ---

// OrderItem is a single line in a distributor ordering operation.
type OrderItem struct {
	SKU               string
	Quantity          int32
	CustomerReference string // partrack order_line_items.id (UUID string)
}

// PartsListResult is returned by CreateList on success.
type PartsListResult struct {
	ListID  string
	ListURL string
}

// QuoteLineItem holds confirmed pricing for one SKU from a quote response.
type QuoteLineItem struct {
	SKU               string
	CustomerReference string // echoed back from the distributor
	UnitPrice         string // decimal string at ordered quantity, e.g. "0.4200"
	QuantityAvailable int32
	IsObsolete        bool
	IsDiscontinued    bool
	ExpirationDate    *time.Time
}

// QuoteResult wraps a confirmed quote from the distributor.
type QuoteResult struct {
	QuoteID   int64
	QuoteName string
	Items     []QuoteLineItem
}

// OrderStatusLineItem holds per-SKU shipment progress.
type OrderStatusLineItem struct {
	SKU               string
	QuantityOrdered   int32
	QuantityShipped   int32
	QuantityBackOrder int32
}

// OrderStatusResult holds the live status of a placed distributor order.
type OrderStatusResult struct {
	SalesOrderID  string
	Status        string
	StatusDetail  string
	LineItems     []OrderStatusLineItem
	LastCheckedAt time.Time
}

// OrderingClient is implemented by distributors that support API ordering flows.
// Check at runtime: if oc, ok := client.(distributor.OrderingClient); ok { ... }
// Credentials JSON may include a "_three_legged_token" field injected by the handler
// for APIs that require 3-legged OAuth.
type OrderingClient interface {
	// CreateList pushes items to a saved parts list on the distributor's platform.
	// Returns (result, listID, err); listID is populated even on partial failure so
	// the caller can persist the orphaned list_id before returning the error.
	CreateList(ctx context.Context, listName string, items []OrderItem, creds json.RawMessage) (*PartsListResult, string, error)
	// RequestQuote creates a quote and fetches confirmed pricing.
	RequestQuote(ctx context.Context, quoteName string, items []OrderItem, creds json.RawMessage) (*QuoteResult, error)
	// GetOrderStatus fetches live order status using 2-legged credentials.
	GetOrderStatus(ctx context.Context, salesOrderID string, creds json.RawMessage) (*OrderStatusResult, error)
}

// --- OAuth types ---

// OAuthTokens holds a token set returned by a 3-legged OAuth exchange.
type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds
}

// ThreeLeggedAuthClient is implemented by distributors that support the OAuth
// authorization-code (3-legged) flow for per-user/per-team token acquisition.
type ThreeLeggedAuthClient interface {
	// AuthorizeURL builds the authorization redirect URL for the user.
	AuthorizeURL(creds json.RawMessage, redirectURI, state string) (string, error)
	// ExchangeCode exchanges an authorization code for access + refresh tokens.
	ExchangeCode(ctx context.Context, creds json.RawMessage, code, redirectURI string) (*OAuthTokens, error)
	// RefreshToken exchanges a refresh token for a new access token.
	RefreshToken(ctx context.Context, creds json.RawMessage, refreshToken string) (*OAuthTokens, error)
}

// AccountIDResolver is implemented by distributors that can resolve the account ID
// for the authenticated user after a 3-legged OAuth exchange. The account ID is
// often required for ordering APIs (e.g. DigiKey's X-DIGIKEY-Account-Id header).
type AccountIDResolver interface {
	// GetPrimaryAccountID fetches the primary account ID for the user identified
	// by accessToken. Returns the account ID string, or an error if unavailable.
	GetPrimaryAccountID(ctx context.Context, creds json.RawMessage, accessToken string) (string, error)
}

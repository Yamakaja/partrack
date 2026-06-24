// Package distributor defines the interface for distributor API integrations
// and the registry that maps api_type strings to Client implementations.
package distributor

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrOAuthRequired is returned by CredentialEnricher.EnrichCredentials when
// re-authorization is needed (no token stored, or refresh failed).
var ErrOAuthRequired = errors.New("oauth_required")

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
	ListingURL     string       // direct URL to this listing's product page; empty if unavailable
	Marketplace    bool         // true = third-party marketplace seller (not stocked by the distributor)
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
	// Name returns the human-readable display name for this distributor (e.g. "DigiKey").
	Name() string
	// CredentialSpec returns the list of fields required to configure this distributor.
	// The frontend uses this to render a typed form instead of a raw JSON textarea.
	// Return nil or an empty slice when no credentials are needed.
	CredentialSpec() []CredentialField

	// EnrichPart fetches full product metadata and listings for a specific distributor SKU.
	// The SKU must be chosen unambiguously by the caller (e.g. selected from SearchParts results).
	// Returns nil, nil if the SKU is not found.
	EnrichPart(ctx context.Context, sku string, credentials json.RawMessage) (*EnrichmentResult, error)
	// RefreshListing fetches current price/stock for sku and returns data for all
	// packaging variants found in the response (distributors typically return all
	// variants for an MPN in a single call). Returns an empty slice when the SKU
	// is not found.
	RefreshListing(ctx context.Context, sku string, credentials json.RawMessage) ([]Listing, error)
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

// OrderStatusShipment holds per-shipment data for one line item.
type OrderStatusShipment struct {
	QuantityShipped      int32      `json:"quantity_shipped"`
	InvoiceID            int64      `json:"invoice_id"`
	ShippedDate          *time.Time `json:"shipped_date,omitempty"`
	TrackingNumber       string     `json:"tracking_number"`
	ExpectedDeliveryDate string     `json:"expected_delivery_date,omitempty"`
}

// OrderStatusSchedule holds a future release schedule entry for a line item.
type OrderStatusSchedule struct {
	ScheduledDate     *time.Time `json:"scheduled_date,omitempty"`
	QuantityScheduled int32      `json:"quantity_scheduled"`
	ReleaseDate       *time.Time `json:"release_date,omitempty"`
}

// OrderStatusAddress holds a shipping address from a distributor order.
type OrderStatusAddress struct {
	Line1       string `json:"line1,omitempty"`
	Line2       string `json:"line2,omitempty"`
	City        string `json:"city,omitempty"`
	State       string `json:"state,omitempty"`
	ZipCode     string `json:"zip_code,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	Phone       string `json:"phone,omitempty"`
	IsoCode     string `json:"iso_code,omitempty"`
}

// OrderStatusContact holds contact information from a distributor order.
type OrderStatusContact struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Email     string `json:"email,omitempty"`
}

// OrderStatusLineItem holds per-SKU shipment progress.
type OrderStatusLineItem struct {
	SKU               string                `json:"sku"`
	MPN               string                `json:"mpn"`
	UnitPrice         float64               `json:"unit_price"`
	QuantityOrdered   int32                 `json:"quantity_ordered"`
	QuantityShipped   int32                 `json:"quantity_shipped"`
	QuantityBackOrder int32                 `json:"quantity_back_order"`
	PackType          string                `json:"pack_type,omitempty"`
	QuantityReserved  int32                 `json:"quantity_reserved"`
	Shipments         []OrderStatusShipment `json:"shipments"`
	Schedules         []OrderStatusSchedule `json:"schedules"`
}

// OrderStatusResult holds the live status of a placed distributor order.
type OrderStatusResult struct {
	SalesOrderID    string
	Status          string
	StatusDetail    string
	PurchaseOrder   string
	DateEntered     time.Time
	LineItems       []OrderStatusLineItem
	LastCheckedAt   time.Time
	OrderNumber     int64
	Currency        string
	TotalPrice      float64
	ShipMethod      string
	ShippingAddress *OrderStatusAddress
	Contact         *OrderStatusContact
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

// StoredToken is a generic OAuth token set as persisted in the oauth_tokens table.
type StoredToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountID    string // optional; empty if not applicable to this distributor
}

// CredentialProvider is a token store abstraction passed to CredentialEnricher.
// The handler constructs a thin adapter scoped to a specific (team, distributor) pair.
type CredentialProvider interface {
	GetOAuthToken(ctx context.Context) (*StoredToken, error)
	StoreOAuthToken(ctx context.Context, tok *StoredToken) error
	DeleteOAuthToken(ctx context.Context) error
}

// CredentialEnricher is optionally implemented by distributors that need
// runtime-resolved credentials (e.g. 3-legged OAuth tokens injected into
// the base 2-legged credential set).
// Check at runtime: if ce, ok := client.(distributor.CredentialEnricher); ok { ... }
type CredentialEnricher interface {
	// EnrichCredentials merges runtime credentials into the base 2-legged
	// credentials JSON, returning enriched credentials ready for ordering API calls.
	// Returns ErrOAuthRequired when re-authorization by the user is needed.
	EnrichCredentials(ctx context.Context, base json.RawMessage, provider CredentialProvider) (json.RawMessage, error)
}

// ThreeLeggedAuthClient is implemented by distributors that support the OAuth
// authorization-code (3-legged) flow for per-user/per-team token acquisition.
// Token refresh is an internal concern of the CredentialEnricher implementation.
type ThreeLeggedAuthClient interface {
	// AuthorizeURL builds the authorization redirect URL for the user.
	AuthorizeURL(creds json.RawMessage, redirectURI, state string) (string, error)
	// ExchangeCode exchanges an authorization code for access + refresh tokens.
	ExchangeCode(ctx context.Context, creds json.RawMessage, code, redirectURI string) (*OAuthTokens, error)
}

// RateLimitStatus holds the most recently observed rate limit headers from a distributor API.
type RateLimitStatus struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RateLimitReporter is optionally implemented by distributor clients that expose
// rate limit headers (e.g. X-Ratelimit-Limit / X-Ratelimit-Remaining).
// Check at runtime: if rl, ok := client.(distributor.RateLimitReporter); ok { ... }
type RateLimitReporter interface {
	// RateLimitStatus returns the last observed rate limit for the credentials,
	// or nil if no request has been made yet with those credentials.
	RateLimitStatus(credentials json.RawMessage) *RateLimitStatus
}

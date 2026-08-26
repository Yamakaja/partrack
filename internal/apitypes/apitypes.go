// Package apitypes holds the wire shapes of the partrack REST API: the JSON the
// handlers marshal and the CLI decodes.
//
// It exists because those two used to be written twice — the handler built an
// anonymous struct inside its function body, and `internal/cli` carried a
// hand-copied mirror of it. Nothing made the copies agree, and one of them was
// wrong: `GET /teams` returns the standard list envelope, the client decoded a
// bare array, and the mismatch survived every test because the test double had
// been written from the same wrong assumption. Sharing the declaration makes
// that class of drift a compile error.
//
// These are transport types, deliberately free of pgtype and database concerns:
// handlers convert their rows into them, and the CLI unmarshals them directly.
// Only the endpoints the CLI consumes live here so far; the rest of the handler
// layer still declares its responses inline.
package apitypes

import "encoding/json"

// List is the standard list envelope — `{total, limit, offset, items}` — used by
// every paginated collection endpoint.
type List[T any] struct {
	Total  int64 `json:"total"`
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
	Items  []T   `json:"items"`
}

// Error is the standard error body.
type Error struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// --- Auth, teams, tokens -----------------------------------------------------

// User is the authenticated user as returned by /auth/me and the auth endpoints.
type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsActive    bool   `json:"is_active"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
}

// Membership is one team membership on the /auth/me response.
type Membership struct {
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
	Role     string `json:"role"`
}

// Me is GET /auth/me.
type Me struct {
	User        User         `json:"user"`
	Memberships []Membership `json:"memberships"`
}

// Team is one row of GET /teams, which wraps these in a List envelope.
type Team struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
	Role        string  `json:"role"`
}

// CreatedToken is POST /tokens. The raw token is returned exactly once.
type CreatedToken struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Token     string  `json:"token"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
}

// --- Categories and the parameter registry -----------------------------------

// Category is one node of the category tree from GET /categories.
type Category struct {
	ID            int32    `json:"id"`
	Slug          string   `json:"slug"`
	DisplayName   string   `json:"display_name"`
	ParentID      *int32   `json:"parent_id"`
	SortOrder     int32    `json:"sort_order"`
	ValueParam    *string  `json:"value_param"`
	PrimaryParams []string `json:"primary_params"`
	GenericCode   *string  `json:"generic_code"`
}

// CategoryList is GET /categories.
type CategoryList struct {
	Categories []Category `json:"categories"`
}

// ParameterDefinition is one canonical parameter from GET /parameter-definitions.
type ParameterDefinition struct {
	ID          int32   `json:"id"`
	Slug        string  `json:"slug"`
	DisplayName string  `json:"display_name"`
	Unit        *string `json:"unit"`
	UnitSymbol  *string `json:"unit_symbol"`
	ValueType   string  `json:"value_type"`
}

// CategoryPartCount is one entry of the team's per-category part counts.
type CategoryPartCount struct {
	ID    int32 `json:"id"`
	Count int32 `json:"count"`
}

// CategoryPartCounts is GET /teams/{teamId}/parts/categories. Counts are direct,
// not recursive — callers aggregate up the tree themselves.
type CategoryPartCounts struct {
	Categories []CategoryPartCount `json:"categories"`
}

// CategoryParameter is one parameter observed on parts in a category subtree,
// from GET /categories/{categoryId}/parameters. Unlike the curated schema this
// is derived from the catalog itself, so it reports the numeric range and value
// vocabulary actually present.
type CategoryParameter struct {
	ID          int32    `json:"id"`
	Slug        string   `json:"slug"`
	DisplayName string   `json:"display_name"`
	Unit        *string  `json:"unit"`
	UnitSymbol  *string  `json:"unit_symbol"`
	ValueType   string   `json:"value_type"`
	MinNumeric  *float64 `json:"min_numeric"`
	MaxNumeric  *float64 `json:"max_numeric"`
	EnumValues  []string `json:"enum_values"`
}

// CategorySchemaParam is one curated parameter, with the catalog-derived value
// vocabulary in Options.
type CategorySchemaParam struct {
	Slug        string   `json:"slug"`
	DisplayName string   `json:"display_name"`
	UnitSymbol  string   `json:"unit_symbol"`
	ValueType   string   `json:"value_type"`
	Options     []string `json:"options"`
}

// CategoryParameterSchema is GET /teams/{teamId}/categories/{categoryId}/parameter-schema.
// ValueParam is the slug whose magnitude a bare value encodes, empty when the
// category is not parametric.
type CategoryParameterSchema struct {
	CategoryID int32                 `json:"category_id"`
	Slug       string                `json:"slug"`
	ValueParam string                `json:"value_param"`
	Params     []CategorySchemaParam `json:"params"`
}

// --- Parts -------------------------------------------------------------------

// PartListItem is one row of GET /parts. The inventory fields are populated only
// when team_id is supplied; a nil InventoryEntryID means the part is not tracked
// by the team, which is a different statement from holding zero.
type PartListItem struct {
	ID                   string            `json:"id"`
	Scope                string            `json:"scope"`
	Mpn                  *string           `json:"mpn"`
	Manufacturer         *string           `json:"manufacturer"`
	Description          *string           `json:"description"`
	ShortDescription     *string           `json:"short_description"`
	Category             *string           `json:"category"`
	CategoryID           *int32            `json:"category_id"`
	CategoryName         *string           `json:"category_name"`
	CategorySource       string            `json:"category_source"`
	Package              *string           `json:"package"`
	LifecycleStatus      string            `json:"lifecycle_status"`
	ImageURL             *string           `json:"image_url"`
	InventoryEntryID     *string           `json:"inventory_entry_id"`
	QuantityOnHand       *int32            `json:"quantity_on_hand"`
	MinimumStockQuantity *int32            `json:"minimum_stock_quantity"`
	ReorderQuantity      *int32            `json:"reorder_quantity"`
	StorageLocationLabel *string           `json:"storage_location_label"`
	StorageCoordinate    []int32           `json:"storage_coordinate"`
	Parameters           map[string]string `json:"parameters,omitempty"`
}

// PartParameter is the common shape of a parameter value on a part.
type PartParameter struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Value                string   `json:"value"`
	Unit                 *string  `json:"unit"`
	ParameterID          *int32   `json:"parameter_id"`
	ParameterSlug        *string  `json:"parameter_slug"`
	ParameterDisplayName *string  `json:"parameter_display_name"`
	ParameterUnitSymbol  *string  `json:"parameter_unit_symbol"`
	NumericValue         *float64 `json:"numeric_value"`
	NumericValueMax      *float64 `json:"numeric_value_max"`
}

// UserParameter is a value the team entered by hand. It carries no source
// distributor, and the embedding means those keys are absent from the JSON
// entirely rather than present and null.
type UserParameter struct {
	PartParameter
}

// DistributorParameter is a value harvested from a distributor listing.
type DistributorParameter struct {
	PartParameter
	SourceDistributorID   string  `json:"source_distributor_id"`
	SourceDistributorName *string `json:"source_distributor_name"`
}

// PartParameters splits a part's parameters by source. User-specified values win
// over distributor-sourced ones; a part's preferred_parameter_source_id resolves
// conflicts between distributors.
type PartParameters struct {
	UserSpecified      []UserParameter        `json:"user_specified"`
	DistributorSourced []DistributorParameter `json:"distributor_sourced"`
}

// Listing is one Distributor × Part × Packaging combination.
type Listing struct {
	ID              string          `json:"id"`
	DistributorID   string          `json:"distributor_id"`
	DistributorName string          `json:"distributor_name"`
	Sku             string          `json:"sku"`
	PackagingType   string          `json:"packaging_type"`
	UnitPrice       *string         `json:"unit_price"`
	StockAvailable  *int32          `json:"stock_available"`
	Moq             int32           `json:"moq"`
	FixedOrderCost  *string         `json:"fixed_order_cost"`
	PriceBreaks     json.RawMessage `json:"price_breaks"`
	LastRefreshedAt *string         `json:"last_refreshed_at"`
}

// PriceBreak is one tier of a listing's quantity pricing, stored as JSONB.
type PriceBreak struct {
	Quantity  int32   `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
}

// ListingList is GET /parts/{partId}/listings.
type ListingList struct {
	Items []Listing `json:"items"`
}

// PartStorageLocation is the inventory entry's location on a part detail, with
// its ancestry already joined.
type PartStorageLocation struct {
	ID           string          `json:"id"`
	Label        string          `json:"label"`
	AncestorPath *string         `json:"ancestor_path"`
	GridConfig   json.RawMessage `json:"grid_config"`
	Coordinate   []int32         `json:"coordinate"`
}

// PartDetail is GET /parts/{partId} — identity, parameters, listings and (when
// team_id is supplied) the team's inventory profile, in one response.
type PartDetail struct {
	ID                         string  `json:"id"`
	Scope                      string  `json:"scope"`
	TeamID                     *string `json:"team_id"`
	Mpn                        *string `json:"mpn"`
	Manufacturer               *string `json:"manufacturer"`
	Description                *string `json:"description"`
	ShortDescription           *string `json:"short_description"`
	Category                   *string `json:"category"`
	CategoryID                 *int32  `json:"category_id"`
	CategoryName               *string `json:"category_name"`
	CategorySource             string  `json:"category_source"`
	CategoryParentID           *int32  `json:"category_parent_id"`
	Package                    *string `json:"package"`
	DatasheetURL               *string `json:"datasheet_url"`
	ProductURL                 *string `json:"product_url"`
	ImageURL                   *string `json:"image_url"`
	LifecycleStatus            string  `json:"lifecycle_status"`
	EnrichmentSourceID         *string `json:"enrichment_source_id"`
	EnrichedAt                 *string `json:"enriched_at"`
	PreferredParameterSourceID *string `json:"preferred_parameter_source_id"`
	CreatedAt                  string  `json:"created_at"`

	Aliases              []string             `json:"aliases"`
	InventoryEntryID     *string              `json:"inventory_entry_id"`
	QuantityOnHand       *int32               `json:"quantity_on_hand"`
	MinimumStockQuantity *int32               `json:"minimum_stock_quantity"`
	ReorderQuantity      *int32               `json:"reorder_quantity"`
	PreferredListingID   *string              `json:"preferred_listing_id"`
	StorageLocation      *PartStorageLocation `json:"storage_location"`

	Parameters PartParameters `json:"parameters"`
	Listings   []Listing      `json:"listings"`
}

// PartAllocation is one active build reservation against a part.
type PartAllocation struct {
	BuildID          string `json:"build_id"`
	ProjectID        string `json:"project_id"`
	ProjectName      string `json:"project_name"`
	BuildStatus      string `json:"build_status"`
	BuildQuantity    int32  `json:"build_quantity"`
	QuantityNeeded   int32  `json:"quantity_needed"`
	QuantityReserved int32  `json:"quantity_reserved"`
	QuantityReceived int32  `json:"quantity_received"`
}

// PartAllocationList is GET /teams/{teamId}/parts/{partId}/allocations.
type PartAllocationList struct {
	Items []PartAllocation `json:"items"`
}

// --- Inventory and storage ---------------------------------------------------

// StockAdjustment is one immutable ledger entry. SourceType is nil for a manual
// correction, which is meaningful rather than missing.
type StockAdjustment struct {
	ID                   string  `json:"id"`
	Delta                int32   `json:"delta"`
	AdjustedAt           string  `json:"adjusted_at"`
	Reason               *string `json:"reason"`
	CreatedBy            string  `json:"created_by"`
	CreatedByDisplayName string  `json:"created_by_display_name"`
	SourceType           *string `json:"source_type"`
	SourceID             *string `json:"source_id"`
	SourceOrderID        *string `json:"source_order_id"`
}

// StorageLocation is one node of the team's storage hierarchy.
type StorageLocation struct {
	ID          string          `json:"id"`
	TeamID      string          `json:"team_id"`
	ParentID    *string         `json:"parent_id"`
	Label       string          `json:"label"`
	Description *string         `json:"description"`
	GridConfig  json.RawMessage `json:"grid_config"`
}

// StorageLocationList is GET /teams/{teamId}/storage-locations.
type StorageLocationList struct {
	Items []StorageLocation `json:"items"`
}

// LocationInventoryItem is one inventory entry stored at (or below) a location.
type LocationInventoryItem struct {
	ID                   string  `json:"id"`
	TeamID               string  `json:"team_id"`
	PartID               string  `json:"part_id"`
	PartMpn              *string `json:"part_mpn"`
	PartManufacturer     *string `json:"part_manufacturer"`
	PartLifecycleStatus  string  `json:"part_lifecycle_status"`
	PartImageURL         *string `json:"part_image_url"`
	PartDescription      *string `json:"part_description"`
	QuantityOnHand       int32   `json:"quantity_on_hand"`
	StorageLocationID    *string `json:"storage_location_id"`
	StorageCoordinate    []int32 `json:"storage_coordinate"`
	StorageLocationLabel *string `json:"storage_location_label"`
	PreferredListingID   *string `json:"preferred_listing_id"`
	MinimumStockQuantity *int32  `json:"minimum_stock_quantity"`
	ReorderQuantity      *int32  `json:"reorder_quantity"`
}

// LocationInventoryList is GET /teams/{teamId}/storage-locations/{locationId}/inventory.
// It predates the standard envelope and carries only items and total — no limit
// or offset — so it gets its own type rather than gaining keys silently.
type LocationInventoryList struct {
	Items []LocationInventoryItem `json:"items"`
	Total int64                   `json:"total"`
}

// --- Notes -------------------------------------------------------------------

// NoteManifestAttachment is a resolved image embedded in a note body.
type NoteManifestAttachment struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Width       *int32 `json:"width,omitempty"`
	Height      *int32 `json:"height,omitempty"`
}

// NoteManifestObject is a normalized, render-ready reference to any linkable
// partrack object — the server computes the label, href, and optional status so
// the frontend chip stays kind-agnostic.
type NoteManifestObject struct {
	Kind   string  `json:"kind"`
	Label  string  `json:"label"`
	Href   string  `json:"href"`
	Status *string `json:"status,omitempty"`
}

// NoteManifest resolves a note body's attachment and object references so the
// renderer needs no per-reference round-trips.
type NoteManifest struct {
	Attachments map[string]NoteManifestAttachment `json:"attachments"`
	Objects     map[string]NoteManifestObject     `json:"objects"`
}

// Note is a team's freeform markdown note on a part or project.
type Note struct {
	Body      string       `json:"body"`
	Manifest  NoteManifest `json:"manifest"`
	Flag      *string      `json:"flag,omitempty"` // part notes only (team opinion flag)
	UpdatedAt *string      `json:"updated_at,omitempty"`
	UpdatedBy *string      `json:"updated_by,omitempty"`
}

// ---------------------------------------------------------------------------
// BOM import
//
// These are request shapes rather than responses: the payloads a client sends
// to import a BOM revision and to preview how its rows will be read. They live
// here for the same reason the response types do — the CLI builds them locally
// (see internal/bomfile) and the handler decodes them, so a single declaration
// keeps the producer and the consumer honest.

// BOMImportItem is one mapped BOM row in the body of
// POST /teams/{teamId}/projects/{projectId}/revisions.
//
// It is the output of column mapping: the raw cells sorted into the roles the
// importer understands. Every optional field is a pointer because "absent" and
// "empty" are different to the resolution tiers — an empty MPN column is not
// the same as no MPN column.
type BOMImportItem struct {
	ReferenceDesignators string  `json:"reference_designators"`
	Quantity             int32   `json:"quantity"`
	RawDescription       *string `json:"raw_description"`
	Manufacturer         *string `json:"manufacturer"`
	MPN                  *string `json:"mpn"`
	Name                 *string `json:"name"`
	SKU                  *string `json:"sku"`
	DistributorName      *string `json:"distributor_name"`

	// ResolvedDistributorID is what turns a SKU into a resolvable identity: the
	// import's SKU tier requires it, so a BOM carrying SKUs with no distributor
	// mapped has every SKU row fall through to the later tiers.
	ResolvedDistributorID *string `json:"resolved_distributor_id"`

	Value     *string `json:"value"`
	Footprint *string `json:"footprint"`

	// Interpretation, when set, overrides the parse the server would do and is
	// persisted as a user (or llm) correction. Left raw so a client can pass one
	// through without depending on the genspec types.
	Interpretation       json.RawMessage `json:"interpretation,omitempty"`
	InterpretationSource *string         `json:"interpretation_source,omitempty"`
}

// BOMInterpretItem is one row in the body of POST /bom/interpret-preview — the
// stateless "how will you read this?" endpoint.
//
// It is deliberately narrower than BOMImportItem: interpretation only looks at
// the fields that carry meaning for the parser. Row is the original
// header→cell map, passed so the LLM fallback can see the columns the mapping
// discarded.
type BOMInterpretItem struct {
	Refdes    string            `json:"refdes"`
	Footprint string            `json:"footprint"`
	Value     string            `json:"value"`
	Mpn       string            `json:"mpn"`
	Sku       string            `json:"sku"`
	Row       map[string]string `json:"row,omitempty"`
}

// BOMResolvePreviewRequest is the body of POST /teams/{teamId}/bom/resolve-preview:
// the mapped rows of a BOM that has not been imported, asked what the import
// would do with them.
//
// The items are the same shape the import itself accepts, so a client checks and
// imports the identical payload rather than building it twice.
type BOMResolvePreviewRequest struct {
	// ProjectID scopes the prior-revision tier. Without it that tier is skipped,
	// and lines an earlier revision already resolved will look unresolved.
	ProjectID string          `json:"project_id,omitempty"`
	Items     []BOMImportItem `json:"items"`
}

// BOMResolveCandidate is one catalog part a line could resolve to.
type BOMResolveCandidate struct {
	PartID         string  `json:"part_id"`
	Mpn            *string `json:"mpn"`
	Manufacturer   *string `json:"manufacturer"`
	Package        *string `json:"package"`
	Description    *string `json:"description"`
	Scope          string  `json:"scope"`
	QuantityOnHand *int32  `json:"quantity_on_hand"`
}

// BOMResolvePreviewItem is what the tier ladder concluded about one line,
// without writing anything.
type BOMResolvePreviewItem struct {
	Index                int    `json:"index"`
	ReferenceDesignators string `json:"reference_designators"`

	// State is the resolution_state the import would record: auto_sku, auto_mpn,
	// auto_mpn_fuzzy, auto_prior_revision, auto_generic, ambiguous, importing,
	// or "" when nothing matched and there is nothing to look up.
	State string `json:"state"`
	// Tier names the rung that decided, so an outcome can be explained rather
	// than just reported.
	Tier string `json:"tier,omitempty"`

	// Part is the part the line would resolve to, when it resolves.
	Part *BOMResolveCandidate `json:"part,omitempty"`
	// Candidates are the parts the ladder was choosing between. Present when the
	// line is ambiguous — this is the set a human would be asked to pick from.
	Candidates []BOMResolveCandidate `json:"candidates,omitempty"`

	// Route and Category come from the interpretation the decision used, so a
	// line that resolved parametrically can be checked against how it was read.
	Route    string `json:"route,omitempty"`
	Category string `json:"category,omitempty"`
	// InterpretationSource is "supplied" (the client sent one), "reused" (the
	// project's remembered correction), or "auto" (a fresh parse).
	InterpretationSource string `json:"interpretation_source"`
}

// BOMResolvePreview is the response: one entry per line, plus a count per state
// so the headline question — how much of this BOM lands automatically — can be
// answered without tallying.
type BOMResolvePreview struct {
	Items   []BOMResolvePreviewItem `json:"items"`
	Summary map[string]int          `json:"summary"`
}

// ProjectListItem is one row of GET /teams/{teamId}/projects. The CLI reads it
// to turn a project name into an id.
type ProjectListItem struct {
	ID                        string  `json:"id"`
	Name                      string  `json:"name"`
	Description               *string `json:"description"`
	Status                    string  `json:"status"`
	ActiveBomRevisionID       *string `json:"active_bom_revision_id"`
	ActiveBomRevisionLabel    *string `json:"active_bom_revision_label"`
	PreferredDistributorID    *string `json:"preferred_distributor_id"`
	SourceURL                 *string `json:"source_url"`
	ProjectManagerID          *string `json:"project_manager_id"`
	ProjectManagerDisplayName *string `json:"project_manager_display_name"`
	CreatedByDisplayName      string  `json:"created_by_display_name"`
	CreatedAt                 string  `json:"created_at"`
}

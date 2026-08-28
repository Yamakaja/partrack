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

// --- Projects and BOM revisions -----------------------------------------------

// RevisionSummary counts how a revision's lines came out of resolution. It rides
// along on a project detail for the active revision, so "is this BOM ready" can
// be answered without a second request.
type RevisionSummary struct {
	ItemCount       int64 `json:"item_count"`
	ResolvedCount   int64 `json:"resolved_count"`
	UnresolvedCount int64 `json:"unresolved_count"`
	AmbiguousCount  int64 `json:"ambiguous_count"`
}

// ProjectDetail is GET /teams/{teamId}/projects/{projectId}. It is ProjectListItem
// plus the import config and the active revision's resolution summary.
type ProjectDetail struct {
	ID                        string           `json:"id"`
	Name                      string           `json:"name"`
	Description               *string          `json:"description"`
	Status                    string           `json:"status"`
	ActiveBomRevisionID       *string          `json:"active_bom_revision_id"`
	ActiveBomRevisionLabel    *string          `json:"active_bom_revision_label"`
	BomImportConfig           json.RawMessage  `json:"bom_import_config"`
	PreferredDistributorID    *string          `json:"preferred_distributor_id"`
	SourceURL                 *string          `json:"source_url"`
	ProjectManagerID          *string          `json:"project_manager_id"`
	ProjectManagerDisplayName *string          `json:"project_manager_display_name"`
	CreatedByDisplayName      string           `json:"created_by_display_name"`
	CreatedAt                 string           `json:"created_at"`
	ActiveRevisionSummary     *RevisionSummary `json:"active_revision_summary"`
}

// RevisionListItem is one row of GET …/projects/{projectId}/revisions.
type RevisionListItem struct {
	ID                    string  `json:"id"`
	Label                 string  `json:"label"`
	ImportedAt            string  `json:"imported_at"`
	Notes                 *string `json:"notes"`
	ImportedByDisplayName string  `json:"imported_by_display_name"`
	ItemCount             int64   `json:"item_count"`
	ResolvedCount         int64   `json:"resolved_count"`
	UnresolvedCount       int64   `json:"unresolved_count"`
	IsActive              bool    `json:"is_active"`
}

// RevisionList is GET …/projects/{projectId}/revisions. It predates the standard
// envelope and does not paginate, so it carries items alone.
type RevisionList struct {
	Items []RevisionListItem `json:"items"`
}

// BOMSubstituteEntry is one approved substitute on a BOM line.
type BOMSubstituteEntry struct {
	PartID  string  `json:"part_id"`
	PartMpn *string `json:"part_mpn"`
}

// BOMResolveItem is the resolve-queue task attached to a BOM line, present only
// while a line is still being looked up or after it failed.
type BOMResolveItem struct {
	ID             string          `json:"id"`
	Status         *string         `json:"status"`
	TaskType       *string         `json:"task_type"`
	EnrichmentData json.RawMessage `json:"enrichment_data,omitempty"`
	ErrorMessage   *string         `json:"error_message,omitempty"`
}

// BOMLineItem is one line of an imported revision: the raw columns as they were
// read, what they resolved to, and the state that decided it.
type BOMLineItem struct {
	ID                   string  `json:"id"`
	ReferenceDesignators string  `json:"reference_designators"`
	Quantity             int32   `json:"quantity"`
	PartName             *string `json:"part_name"`
	RawDescription       *string `json:"raw_description"`
	Manufacturer         *string `json:"manufacturer"`
	MPN                  *string `json:"mpn"`
	SKU                  *string `json:"sku"`
	DistributorName      string  `json:"distributor_name"`
	HasLinkedDistributor bool    `json:"has_linked_distributor"`
	ResolvedPartID       *string `json:"resolved_part_id"`
	ResolvedPartMpn      *string `json:"resolved_part_mpn"`
	ResolvedPartFlag     *string `json:"resolved_part_flag,omitempty"`
	ResolvedPartMfr      *string `json:"resolved_part_manufacturer"`
	ResolvedPartDesc     *string `json:"resolved_part_description"`
	ResolvedPartPackage  *string `json:"resolved_part_package"`
	ResolvedPartLifecyle *string `json:"resolved_part_lifecycle_status"`
	ResolvedPartImageURL *string `json:"resolved_part_image_url"`
	QuantityOnHand       *int32  `json:"quantity_on_hand"`
	ResolutionState      *string `json:"resolution_state"`

	// Where an inherited resolution actually came from — the decision that chose
	// the part, already collapsed to one hop by the import worker. Null unless
	// resolution_state is auto_prior_revision.
	OriginResolutionState *string `json:"origin_resolution_state,omitempty"`
	OriginRevisionLabel   *string `json:"origin_revision_label,omitempty"`

	Ignored              bool                 `json:"ignored"`
	Value                *string              `json:"value,omitempty"`
	Footprint            *string              `json:"footprint,omitempty"`
	Interpretation       json.RawMessage      `json:"interpretation,omitempty"`
	InterpretationSource string               `json:"interpretation_source,omitempty"`
	Substitutes          []BOMSubstituteEntry `json:"substitutes"`
	ResolveItem          *BOMResolveItem      `json:"resolve_item,omitempty"`
}

// RevisionDetail is GET …/revisions/{revId}.
type RevisionDetail struct {
	ID                    string        `json:"id"`
	Label                 string        `json:"label"`
	ImportedAt            string        `json:"imported_at"`
	Notes                 *string       `json:"notes"`
	ImportedByDisplayName string        `json:"imported_by_display_name"`
	IsActive              bool          `json:"is_active"`
	Items                 []BOMLineItem `json:"items"`
}

// --- Stock check ---------------------------------------------------------------

// StockCheckItem is one BOM line measured against the team's stock for a given
// build quantity. Build quantity is ephemeral — a parameter of the question, not
// a property of the project.
type StockCheckItem struct {
	BomLineItemID        string  `json:"bom_line_item_id"`
	ReferenceDesignators string  `json:"reference_designators"`
	QuantityPerBoard     int32   `json:"quantity_per_board"`
	RequiredQuantity     int32   `json:"required_quantity"`
	QuantityOnHand       int32   `json:"quantity_on_hand"`
	QuantityReserved     int32   `json:"quantity_reserved"`
	QuantityFree         int32   `json:"quantity_free"`
	QuantityOnOrder      int32   `json:"quantity_on_order"`
	Shortfall            int32   `json:"shortfall"`
	StockStatus          string  `json:"stock_status"`
	ResolvedPartID       *string `json:"resolved_part_id"`
	ResolvedPartMpn      *string `json:"resolved_part_mpn"`
	MinStockQty          *int32  `json:"min_stock_qty"`
	BelowRestock         bool    `json:"below_restock"`

	// Substitute fields — nil when using the primary part.
	SelectedSubstituteID  *string `json:"selected_substitute_id"`
	SelectedSubstituteMPN *string `json:"selected_substitute_mpn"`
	SubstituteQtyOnHand   *int32  `json:"substitute_qty_on_hand"`
}

// StockCheck is GET …/revisions/{revId}/stock-check. FullyBuildable is how many
// complete boards the current stock covers.
type StockCheck struct {
	RevisionID     string           `json:"revision_id"`
	BuildQuantity  int32            `json:"build_quantity"`
	FullyBuildable int32            `json:"fully_buildable"`
	Items          []StockCheckItem `json:"items"`
}

// --- Orders --------------------------------------------------------------------

// OrderListItem is one row of GET /teams/{teamId}/orders.
type OrderListItem struct {
	ID                   string  `json:"id"`
	DistributorID        string  `json:"distributor_id"`
	DistributorName      string  `json:"distributor_name"`
	Status               string  `json:"status"`
	ExternalReference    *string `json:"external_reference"`
	CreatedAt            string  `json:"created_at"`
	CreatedByDisplayName string  `json:"created_by_display_name"`
	LineItemCount        int64   `json:"line_item_count"`
	TotalPrice           *string `json:"total_price"`
}

// Order is the header of a purchase order. Every nullable field is a pointer
// without omitempty: the keys are always present, and absent reads as null.
type Order struct {
	ID                  string  `json:"id"`
	DistributorID       string  `json:"distributor_id"`
	DistributorName     string  `json:"distributor_name"`
	DistributorApiType  *string `json:"distributor_api_type"`
	Status              string  `json:"status"`
	ExternalReference   *string `json:"external_reference"`
	Notes               *string `json:"notes"`
	PurchaseOrderNumber *string `json:"purchase_order_number"`
	CreatedAt           string  `json:"created_at"`

	CreatedByDisplayName string  `json:"created_by_display_name"`
	SubmissionMethod     *string `json:"submission_method"`
	ExternalRefLinkedAt  *string `json:"external_ref_linked_at"`
	ReceivedAt           *string `json:"received_at"`

	// Integrations is the distributor-side state cached on the order. It defaults
	// to an empty object rather than null, so a client can index into it without
	// a nil check.
	Integrations json.RawMessage `json:"integrations"`
}

// OrderLineItem is one line of an order, with the price snapshot taken when the
// line was created — not the listing's price today.
type OrderLineItem struct {
	ID                     string  `json:"id"`
	ListingID              string  `json:"listing_id"`
	PartID                 string  `json:"part_id"`
	PartMpn                *string `json:"part_mpn"`
	PartManufacturer       *string `json:"part_manufacturer"`
	PartLifecycleStatus    string  `json:"part_lifecycle_status"`
	PartImageURL           *string `json:"part_image_url"`
	PartDescription        *string `json:"part_description"`
	Sku                    string  `json:"sku"`
	PackagingType          string  `json:"packaging_type"`
	QuantityOrdered        int32   `json:"quantity_ordered"`
	QuantityReceived       int32   `json:"quantity_received"`
	UnitPriceSnapshot      *string `json:"unit_price_snapshot"`
	FixedOrderCostSnapshot *string `json:"fixed_order_cost_snapshot"`
}

// OrderDetail is GET …/orders/{orderId}: the header flattened alongside its lines.
type OrderDetail struct {
	Order
	LineItemCount int             `json:"line_item_count"`
	Items         []OrderLineItem `json:"items"`
}

// LinkedBuild is one build an order was raised for. GET …/orders/{orderId}/linked-builds
// returns these as a bare array, not a list envelope.
type LinkedBuild struct {
	ID                   string  `json:"id"`
	ProjectID            string  `json:"project_id"`
	ProjectName          string  `json:"project_name"`
	BomRevisionID        string  `json:"bom_revision_id"`
	BomRevisionLabel     string  `json:"bom_revision_label"`
	Quantity             int32   `json:"quantity"`
	Status               string  `json:"status"`
	CreatedAt            string  `json:"created_at"`
	CreatedByDisplayName *string `json:"created_by_display_name"`
	TotalLineItems       int32   `json:"total_line_items"`
	FulfilledLineItems   int32   `json:"fulfilled_line_items"`
}

// --- Builds --------------------------------------------------------------------

// BuildListItem is one row of GET /teams/{teamId}/builds.
type BuildListItem struct {
	ID                     string  `json:"id"`
	ProjectID              string  `json:"project_id"`
	ProjectName            string  `json:"project_name"`
	BomRevisionID          string  `json:"bom_revision_id"`
	BomRevisionLabel       string  `json:"bom_revision_label"`
	Quantity               int32   `json:"quantity"`
	Status                 string  `json:"status"`
	AssembledAt            *string `json:"assembled_at"`
	CreatedAt              string  `json:"created_at"`
	Notes                  *string `json:"notes"`
	CreatedBy              string  `json:"created_by"`
	CreatedByDisplayName   *string `json:"created_by_display_name"`
	AssembledByDisplayName *string `json:"assembled_by_display_name"`
}

// PipelineBuild is one build in GET …/builds/pipeline, which groups the active
// (non-terminal) builds by status.
type PipelineBuild struct {
	ID                   string  `json:"id"`
	ProjectID            string  `json:"project_id"`
	ProjectName          string  `json:"project_name"`
	BomRevisionID        string  `json:"bom_revision_id"`
	BomRevisionLabel     string  `json:"bom_revision_label"`
	Quantity             int32   `json:"quantity"`
	Status               string  `json:"status"`
	CreatedAt            string  `json:"created_at"`
	Notes                *string `json:"notes"`
	CreatedBy            string  `json:"created_by"`
	CreatedByDisplayName *string `json:"created_by_display_name"`
	ExcludedFromCart     bool    `json:"excluded_from_cart"`
}

// BuildAllocation is one soft reservation a build holds against a part.
type BuildAllocation struct {
	ID                  string  `json:"id"`
	PartID              string  `json:"part_id"`
	PartMpn             *string `json:"part_mpn"`
	PartManufacturer    *string `json:"part_manufacturer"`
	PartLifecycleStatus string  `json:"part_lifecycle_status"`
	PartImageURL        *string `json:"part_image_url"`
	PartDescription     *string `json:"part_description"`
	QuantityNeeded      int32   `json:"quantity_needed"`
	QuantityReserved    int32   `json:"quantity_reserved"`
	QuantityReceived    int32   `json:"quantity_received"`
}

// BuildConsumedItem is one stock deduction an assembled build made. It is null on
// the wire until the build is assembled, which is what distinguishes "nothing was
// consumed" from "this build has not been built yet".
type BuildConsumedItem struct {
	InventoryEntryID    string  `json:"inventory_entry_id"`
	PartID              string  `json:"part_id"`
	PartMpn             *string `json:"part_mpn"`
	PartManufacturer    *string `json:"part_manufacturer"`
	PartLifecycleStatus string  `json:"part_lifecycle_status"`
	PartImageURL        *string `json:"part_image_url"`
	PartDescription     *string `json:"part_description"`
	QuantityDeducted    int32   `json:"quantity_deducted"`
}

// BuildSubstituteSelection records that a build will use a substitute rather than
// the BOM line's primary part.
type BuildSubstituteSelection struct {
	BomLineItemID   string  `json:"bom_line_item_id"`
	SelectedPartID  string  `json:"selected_part_id"`
	SelectedPartMpn *string `json:"selected_part_mpn"`
}

// BuildLineItemOverride records a per-line decision (e.g. do not source) taken on
// a build.
type BuildLineItemOverride struct {
	BomLineItemID string `json:"bom_line_item_id"`
	OverrideType  string `json:"override_type"`
}

// BuildKittingSession is the most recent non-abandoned kitting session, so the
// desktop can restore the kitting panel after a reload.
type BuildKittingSession struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// BuildDetail is GET …/builds/{buildId}.
type BuildDetail struct {
	ID                     string   `json:"id"`
	ProjectID              string   `json:"project_id"`
	ProjectName            string   `json:"project_name"`
	BomRevisionID          string   `json:"bom_revision_id"`
	BomRevisionLabel       string   `json:"bom_revision_label"`
	Quantity               int32    `json:"quantity"`
	Status                 string   `json:"status"`
	Notes                  *string  `json:"notes"`
	ExcludedFromCart       bool     `json:"excluded_from_cart"`
	ExcludedDesignators    []string `json:"excluded_designators"`
	CreatedAt              string   `json:"created_at"`
	CreatedByDisplayName   *string  `json:"created_by_display_name"`
	AssembledByDisplayName *string  `json:"assembled_by_display_name"`
	AssembledQuantity      *int32   `json:"assembled_quantity"`
	AssembledAt            *string  `json:"assembled_at"`

	Allocations []BuildAllocation `json:"allocations"`
	OrderIDs    []string          `json:"order_ids"`
	// Items are the stock deductions an assembled build made — null until then.
	Items []BuildConsumedItem `json:"items"`

	SelectionsStale      bool                       `json:"selections_stale"`
	SelectionsCheckedAt  *string                    `json:"selections_checked_at"`
	SubstituteSelections []BuildSubstituteSelection `json:"substitute_selections"`
	LineItemOverrides    []BuildLineItemOverride    `json:"line_item_overrides"`
	KittingSession       *BuildKittingSession       `json:"kitting_session"`
}

// BuildPreviewSubstitute is one substitute offered for a previewed BOM line.
type BuildPreviewSubstitute struct {
	PartID          string  `json:"part_id"`
	PartMpn         *string `json:"part_mpn"`
	PartDescription *string `json:"part_description"`
	QuantityOnHand  int32   `json:"quantity_on_hand"`
}

// BuildPreviewItem is one BOM line as the build will actually consume it, with
// the build's excluded designators, line overrides and substitute selections
// already applied.
type BuildPreviewItem struct {
	BomLineItemID               string  `json:"bom_line_item_id"`
	ReferenceDesignators        string  `json:"reference_designators"`
	ResolvedPartID              *string `json:"resolved_part_id"`
	ResolvedPartMpn             *string `json:"resolved_part_mpn"`
	ResolvedPartManufacturer    *string `json:"resolved_part_manufacturer"`
	ResolvedPartLifecycleStatus *string `json:"resolved_part_lifecycle_status"`
	ResolvedPartImageURL        *string `json:"resolved_part_image_url"`
	ResolvedPartDescription     *string `json:"resolved_part_description"`
	QuantityPerBoard            int32   `json:"quantity_per_board"`
	EffectiveQuantityPerBoard   int32   `json:"effective_quantity_per_board"`
	TotalQuantity               int32   `json:"total_quantity"`
	EffectiveTotalQuantity      int32   `json:"effective_total_quantity"`
	QuantityOnHand              int32   `json:"quantity_on_hand"`
	Sufficient                  bool    `json:"sufficient"`
	ExcludedDesignatorCount     int     `json:"excluded_designator_count"`
	Override                    *string `json:"override"`
	AvailableQuantity           *int32  `json:"available_quantity"`

	Substitutes []BuildPreviewSubstitute `json:"substitutes"`
}

// BuildPreview is GET …/builds/{buildId}/preview.
type BuildPreview struct {
	Items []BuildPreviewItem `json:"items"`
}

// --- Cart ----------------------------------------------------------------------

// CartListing is the listing a cart item will be ordered from. Chosen=true means
// the user set it explicitly; false means the sourcing policy resolved it, and
// Reason says which rule applied.
type CartListing struct {
	ListingID       string          `json:"listing_id"`
	DistributorID   *string         `json:"distributor_id"`
	DistributorName *string         `json:"distributor_name"`
	SKU             *string         `json:"sku"`
	UnitPrice       *string         `json:"unit_price"`
	FixedOrderCost  *string         `json:"fixed_order_cost"`
	MOQ             int32           `json:"moq"`
	PriceBreaks     json.RawMessage `json:"price_breaks"`
	PackagingType   *string         `json:"packaging_type"`
	StockAvailable  *int32          `json:"stock_available"`
	LastRefreshedAt *string         `json:"last_refreshed_at"`
	OptimizedQty    int32           `json:"optimized_qty"`
	Chosen          bool            `json:"chosen"`

	// Reason explains an auto-resolved selection under the sourcing policy (empty
	// when the user chose the listing). PreferredStale flags a preference honoured
	// on stale stock.
	Reason         string `json:"reason,omitempty"`
	PreferredStale bool   `json:"preferred_stale,omitempty"`
}

// CartBuildDemand attributes part of a cart item's quantity to a pending build.
type CartBuildDemand struct {
	BuildID          string `json:"build_id"`
	ProjectName      string `json:"project_name"`
	BomRevisionLabel string `json:"bom_revision_label"`
	QuantityNeeded   int32  `json:"quantity_needed"`
}

// CartItem is one staged part in the team's shopping cart.
type CartItem struct {
	ID                   string  `json:"id"`
	PartID               string  `json:"part_id"`
	PartMpn              *string `json:"part_mpn"`
	PartDescription      *string `json:"part_description"`
	PartShortDescription *string `json:"part_short_description"`
	PartManufacturer     *string `json:"part_manufacturer"`
	PartLifecycleStatus  string  `json:"part_lifecycle_status"`
	PartCategory         *string `json:"part_category"`
	PartPackage          *string `json:"part_package"`
	PartProductURL       *string `json:"part_product_url"`
	PartDatasheetURL     *string `json:"part_datasheet_url"`
	PartImageURL         *string `json:"part_image_url"`
	Quantity             int32   `json:"quantity"`
	Source               string  `json:"source"`
	SourceBomLineItemID  *string `json:"source_bom_line_item_id"`

	Listing      *CartListing      `json:"listing"`
	BuildDemands []CartBuildDemand `json:"build_demands"`
}

// Cart is GET /teams/{teamId}/cart. There is one cart per team.
type Cart struct {
	Items []CartItem `json:"items"`
}

// --- Checkouts -----------------------------------------------------------------

// CheckoutListItem is one row of GET /teams/{teamId}/checkouts.
type CheckoutListItem struct {
	ID                     string  `json:"id"`
	PerformedAt            string  `json:"performed_at"`
	Reason                 *string `json:"reason"`
	PerformedByDisplayName string  `json:"performed_by_display_name"`
	ItemCount              int64   `json:"item_count"`
}

// CheckoutLineItem is one part withdrawn by a general checkout, with the ledger
// entry the withdrawal produced.
type CheckoutLineItem struct {
	PartID            string  `json:"part_id"`
	PartMpn           *string `json:"part_mpn"`
	Quantity          int32   `json:"quantity"`
	StockAdjustmentID *string `json:"stock_adjustment_id"`
}

// CheckoutDetail is GET …/checkouts/{checkoutId}.
type CheckoutDetail struct {
	ID                     string             `json:"id"`
	PerformedAt            string             `json:"performed_at"`
	Reason                 *string            `json:"reason"`
	PerformedByDisplayName string             `json:"performed_by_display_name"`
	Items                  []CheckoutLineItem `json:"items"`
}

package cli

import "github.com/Yamakaja/partrack/internal/apitypes"

// The CLI decodes the server's own response declarations from
// internal/apitypes rather than a hand-written mirror of them. These aliases
// keep the short local names the commands read with, while making any change to
// a handler's wire shape a compile error here instead of a runtime decode
// failure against a live server.
type (
	PartListItem        = apitypes.PartListItem
	PartDetail          = apitypes.PartDetail
	PartParameter       = apitypes.PartParameter
	Listing             = apitypes.Listing
	PriceBreak          = apitypes.PriceBreak
	StorageLocationRef  = apitypes.PartStorageLocation
	Team                = apitypes.Team
	CategorySchema      = apitypes.CategoryParameterSchema
	CategorySchemaParam = apitypes.CategorySchemaParam
	StockAdjustment     = apitypes.StockAdjustment
	PartAllocation      = apitypes.PartAllocation
	StorageLocation     = apitypes.StorageLocation
	Category            = apitypes.Category
	ParamDef            = apitypes.ParameterDefinition
	Note                = apitypes.Note

	ProjectListItem  = apitypes.ProjectListItem
	ProjectDetail    = apitypes.ProjectDetail
	RevisionListItem = apitypes.RevisionListItem
	RevisionDetail   = apitypes.RevisionDetail
	BOMLineItem      = apitypes.BOMLineItem
	StockCheck       = apitypes.StockCheck
	StockCheckItem   = apitypes.StockCheckItem
	OrderListItem    = apitypes.OrderListItem
	OrderDetail      = apitypes.OrderDetail
	OrderLineItem    = apitypes.OrderLineItem
	LinkedBuild      = apitypes.LinkedBuild
	BuildListItem    = apitypes.BuildListItem
	BuildDetail      = apitypes.BuildDetail
	BuildPreviewItem = apitypes.BuildPreviewItem
	PipelineBuild    = apitypes.PipelineBuild
	CartItem         = apitypes.CartItem
	CartListing      = apitypes.CartListing
	CheckoutListItem = apitypes.CheckoutListItem
	CheckoutDetail   = apitypes.CheckoutDetail
)

// List is the standard {total, limit, offset, items} envelope.
type List[T any] = apitypes.List[T]

// PartListResponse is the standard list envelope around part rows.
type PartListResponse = apitypes.List[PartListItem]

// identOf extracts the addressing subset for the @N / prefix state file.
func identOf(id string, mpn, manufacturer *string) Ref {
	return Ref{
		Kind:         kindPart,
		ID:           id,
		Label:        strOr(mpn, ""),
		Manufacturer: strOr(manufacturer, ""),
	}
}

// partIdent is the addressing subset of a list row.
func partIdent(p PartListItem) Ref {
	return identOf(p.ID, p.Mpn, p.Manufacturer)
}

// detailIdent is the addressing subset of a part detail.
func detailIdent(p *PartDetail) Ref {
	return identOf(p.ID, p.Mpn, p.Manufacturer)
}

// displayName is the label used in headings and error lists.
func displayName(p *PartDetail) string {
	if p.Mpn != nil && *p.Mpn != "" {
		return *p.Mpn
	}
	if p.Description != nil && *p.Description != "" {
		return *p.Description
	}
	return p.ID
}

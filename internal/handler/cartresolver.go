package handler

import (
	"context"
	"encoding/json"
	"math/big"
	"sync"

	"github.com/Yamakaja/partrack/internal/distributor"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

// resolvedListing holds the result of auto-resolving a cart item to a listing.
type resolvedListing struct {
	ListingID       pgtype.UUID
	DistributorID   pgtype.UUID
	DistributorName string
	SKU             string
	Preferred       bool
	UnitPriceAtQty  pgtype.Numeric // effective unit price after applying price breaks
	FixedOrderCost  pgtype.Numeric
	MOQ             int32
	PriceBreaks     []byte // raw JSONB — all price breaks for the listing
	PackagingType   string
	StockAvailable  *int32
}

// teamCartResolution maps cart item ID (string) to its resolved listing.
// Only items with a null listing_id appear here.
type teamCartResolution struct {
	ByItemID map[string]*resolvedListing // keyed by cart_line_item.id string
}

// CartResolver caches per-team listing resolutions in memory.
// It is safe for concurrent use.
type CartResolver struct {
	mu      sync.RWMutex
	entries map[string]*teamCartResolution // keyed by team UUID string
}

// NewCartResolver creates an empty CartResolver.
func NewCartResolver() *CartResolver {
	return &CartResolver{entries: make(map[string]*teamCartResolution)}
}

// Invalidate removes the cached resolution for a team.
// Call this after any cart mutation.
func (cr *CartResolver) Invalidate(teamID string) {
	cr.mu.Lock()
	delete(cr.entries, teamID)
	cr.mu.Unlock()
}

// Resolve returns (possibly cached) resolved listings for cart items with a null listing_id.
// Items that already have a listing_id set are not resolved and will not appear in the result.
func (cr *CartResolver) Resolve(
	ctx context.Context,
	teamIDStr string,
	items []dbgen.GetCartItemsRow,
	queries *dbgen.Queries,
) (*teamCartResolution, error) {
	cr.mu.RLock()
	if res, ok := cr.entries[teamIDStr]; ok {
		cr.mu.RUnlock()
		return res, nil
	}
	cr.mu.RUnlock()

	var teamID pgtype.UUID
	if err := teamID.Scan(teamIDStr); err != nil {
		return nil, err
	}

	// Collect unique part IDs for items that lack an explicit listing.
	partIDSet := make(map[string]pgtype.UUID)
	itemsByPartID := make(map[string][]dbgen.GetCartItemsRow)
	for _, item := range items {
		if item.ListingID.Valid {
			continue
		}
		pid := item.PartID.String()
		partIDSet[pid] = item.PartID
		itemsByPartID[pid] = append(itemsByPartID[pid], item)
	}

	res := &teamCartResolution{ByItemID: make(map[string]*resolvedListing)}

	if len(partIDSet) > 0 {
		partIDs := make([]pgtype.UUID, 0, len(partIDSet))
		for _, pid := range partIDSet {
			partIDs = append(partIDs, pid)
		}

		rows, err := queries.GetListingsForCartResolution(ctx, dbgen.GetListingsForCartResolutionParams{
			TeamID:  teamID,
			PartIds: partIDs,
		})
		if err != nil {
			return nil, err
		}

		// Group listing rows by part_id.
		listingsByPart := make(map[string][]dbgen.GetListingsForCartResolutionRow)
		for _, row := range rows {
			pid := row.PartID.String()
			listingsByPart[pid] = append(listingsByPart[pid], row)
		}

		for partIDStr, cartItems := range itemsByPartID {
			listings := listingsByPart[partIDStr]
			if len(listings) == 0 {
				continue // no enabled listings for this part
			}
			for _, cartItem := range cartItems {
				if best := pickBestListing(listings, cartItem.Quantity); best != nil {
					res.ByItemID[cartItem.ID.String()] = best
				}
			}
		}
	}

	cr.mu.Lock()
	cr.entries[teamIDStr] = res
	cr.mu.Unlock()
	return res, nil
}

// pickBestListing selects the listing with the lowest total cost for the given cart quantity,
// taking price breaks, MOQ, and fixed order costs into account.
// Tie-break: preferred distributor wins; then alphabetical by distributor name.
// Returns nil if no listing has a usable price.
func pickBestListing(listings []dbgen.GetListingsForCartResolutionRow, cartQty int32) *resolvedListing {
	var best *resolvedListing
	var bestTotal *big.Float

	for _, l := range listings {
		// Effective quantity must honour the listing's MOQ.
		effQty := cartQty
		if l.Moq > effQty {
			effQty = l.Moq
		}

		// Find the best applicable price break: highest BreakQty that does not exceed effQty.
		var breaks []distributor.PriceBreak
		if len(l.PriceBreaks) > 0 {
			_ = json.Unmarshal(l.PriceBreaks, &breaks)
		}
		var unitPriceStr string
		bestBreak := int32(-1)
		for _, pb := range breaks {
			if pb.BreakQty <= effQty && pb.BreakQty > bestBreak {
				unitPriceStr = pb.UnitPrice
				bestBreak = pb.BreakQty
			}
		}

		// Parse unit price: price break string first, then fall back to the DB column.
		var unitPrice *big.Float
		if unitPriceStr != "" {
			unitPrice, _ = new(big.Float).SetString(unitPriceStr)
		}
		if unitPrice == nil && l.UnitPrice.Valid {
			if s := numericString(l.UnitPrice); s != nil {
				unitPrice, _ = new(big.Float).SetString(*s)
			}
		}
		if unitPrice == nil {
			continue // no usable price; skip this listing
		}

		// Parse fixed order cost (treat as 0 if absent).
		fixedCost := new(big.Float)
		if l.FixedOrderCost.Valid {
			if s := numericString(l.FixedOrderCost); s != nil {
				fixedCost, _ = new(big.Float).SetString(*s)
			}
		}

		total := new(big.Float).Mul(unitPrice, new(big.Float).SetInt64(int64(effQty)))
		total.Add(total, fixedCost)

		// Build effective unit price as pgtype.Numeric for the response.
		var unitPriceN pgtype.Numeric
		if unitPriceStr != "" {
			_ = unitPriceN.Scan(unitPriceStr)
		}
		if !unitPriceN.Valid {
			unitPriceN = l.UnitPrice
		}

		candidate := &resolvedListing{
			ListingID:       l.ListingID,
			DistributorID:   l.DistributorID,
			DistributorName: l.DistributorName,
			SKU:             l.Sku,
			Preferred:       l.Preferred,
			UnitPriceAtQty:  unitPriceN,
			FixedOrderCost:  l.FixedOrderCost,
			MOQ:             l.Moq,
			PriceBreaks:     l.PriceBreaks,
			PackagingType:   l.PackagingType,
			StockAvailable:  l.StockAvailable,
		}

		if best == nil {
			best, bestTotal = candidate, total
			continue
		}

		cmp := total.Cmp(bestTotal)
		switch {
		case cmp < 0:
			best, bestTotal = candidate, total
		case cmp == 0 && l.Preferred && !best.Preferred:
			best, bestTotal = candidate, total
		case cmp == 0 && l.Preferred == best.Preferred && l.DistributorName < best.DistributorName:
			best, bestTotal = candidate, total
		}
	}
	return best
}

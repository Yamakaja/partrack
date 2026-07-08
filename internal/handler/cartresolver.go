package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/timing"
	"github.com/jackc/pgx/v5"
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
	LastRefreshedAt pgtype.Timestamptz
	OptimizedQty    int32 // quantity that minimises total cost; may exceed cart quantity if a price break makes it cheaper

	// Reason explains why this listing was chosen; drives the cart UI's decision chip.
	// One of: "cost", "only_option", "preferred", "preferred_fallback",
	// "out_of_stock_fallback", "over_tolerance".
	Reason string
	// PreferredStale is set when the preference was honoured on stock data that was
	// missing or older than the distributor's cache window (an optimistic pick to flag).
	PreferredStale bool
}

// sourcingPolicy is a team's resolved distributor-selection policy.
type sourcingPolicy struct {
	strategy     string     // "cost" (default) or "preferred_in_stock"
	tolerancePct *big.Float // optional cost guardrail (percent); nil = unset
	toleranceAbs *big.Float // optional cost guardrail (absolute); nil = unset
}

// loadSourcingPolicy reads the team's sourcing settings, defaulting to the cost
// strategy when no row exists so existing teams keep the previous behaviour.
func loadSourcingPolicy(ctx context.Context, teamID pgtype.UUID, queries *dbgen.Queries) (sourcingPolicy, error) {
	row, err := queries.GetTeamSourcingSettings(ctx, teamID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sourcingPolicy{strategy: "cost"}, nil
		}
		return sourcingPolicy{}, err
	}
	p := sourcingPolicy{strategy: row.Strategy}
	if s := numericString(row.CostTolerancePct); s != nil {
		p.tolerancePct, _ = new(big.Float).SetString(*s)
	}
	if s := numericString(row.CostToleranceAbs); s != nil {
		p.toleranceAbs, _ = new(big.Float).SetString(*s)
	}
	return p, nil
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

	if sp := timing.FromContext(ctx); sp != nil {
		s := sp.Start("cart_resolve", "cart listing resolution")
		defer s.Stop()
	}

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
		policy, err := loadSourcingPolicy(ctx, teamID, queries)
		if err != nil {
			return nil, err
		}

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

		now := time.Now()
		for partIDStr, cartItems := range itemsByPartID {
			listings := listingsByPart[partIDStr]
			if len(listings) == 0 {
				continue // no enabled listings for this part
			}
			for _, cartItem := range cartItems {
				if best := pickBestListing(listings, cartItem.Quantity, policy, now); best != nil {
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

// optimalOrderQty returns the order quantity that minimises unit_price × qty by checking
// whether any higher price break reduces total spend versus ordering exactly effQty.
// cartQty is the raw demand; moq is the listing's minimum order quantity.
// The returned quantity is always >= max(cartQty, moq).
func optimalOrderQty(cartQty, moq int32, priceBreaksJSON []byte, baseUnitPrice pgtype.Numeric) int32 {
	effQty := max(cartQty, moq)

	var breaks []distributor.PriceBreak
	if len(priceBreaksJSON) > 0 {
		_ = json.Unmarshal(priceBreaksJSON, &breaks)
	}
	if len(breaks) == 0 {
		return effQty
	}

	// Find the unit price that applies at effQty.
	var naturalPrice *big.Float
	bestBreak := int32(-1)
	for _, pb := range breaks {
		if pb.BreakQty <= effQty && pb.BreakQty > bestBreak {
			naturalPrice, _ = new(big.Float).SetString(pb.UnitPrice)
			bestBreak = pb.BreakQty
		}
	}
	if naturalPrice == nil && baseUnitPrice.Valid {
		if s := numericString(baseUnitPrice); s != nil {
			naturalPrice, _ = new(big.Float).SetString(*s)
		}
	}
	if naturalPrice == nil {
		return effQty
	}

	optQty := effQty
	bestCost := new(big.Float).Mul(naturalPrice, new(big.Float).SetInt64(int64(effQty)))
	cap := cartQty * 100
	for _, pb := range breaks {
		if pb.BreakQty <= effQty || pb.BreakQty > cap {
			continue
		}
		breakPrice, ok := new(big.Float).SetString(pb.UnitPrice)
		if !ok {
			continue
		}
		candidateCost := new(big.Float).Mul(breakPrice, new(big.Float).SetInt64(int64(pb.BreakQty)))
		if candidateCost.Cmp(bestCost) < 0 {
			bestCost = candidateCost
			optQty = pb.BreakQty
		}
	}
	return optQty
}

// listingEval pairs a resolved listing with the data needed to rank it under a policy.
type listingEval struct {
	candidate *resolvedListing
	total     *big.Float // lowest achievable total cost for this listing at the cart quantity
	priority  *int32     // distributor rank (lower = more preferred); nil = unranked
	qualifies bool       // acceptable to honour the preference on (in stock, or optimistically stale)
	stale     bool       // preference would be honoured on missing/stale stock data
}

// evalListing computes the lowest-total-cost candidate for a single listing at the given cart
// quantity, honouring MOQ, price breaks, and fixed order cost. OptimizedQty reflects the quantity
// that minimises total cost. Returns (nil, nil) when the listing has no usable price.
func evalListing(l dbgen.GetListingsForCartResolutionRow, cartQty int32) (*resolvedListing, *big.Float) {
	// Effective quantity must honour the listing's MOQ.
	effQty := max(cartQty, l.Moq)

	// Parse price breaks once.
	var breaks []distributor.PriceBreak
	if len(l.PriceBreaks) > 0 {
		_ = json.Unmarshal(l.PriceBreaks, &breaks)
	}

	// Find the best applicable price break: highest BreakQty that does not exceed effQty.
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
		return nil, nil // no usable price; skip this listing
	}

	// Parse fixed order cost (treat as 0 if absent).
	fixedCost := new(big.Float)
	if l.FixedOrderCost.Valid {
		if s := numericString(l.FixedOrderCost); s != nil {
			fixedCost, _ = new(big.Float).SetString(*s)
		}
	}

	naturalTotal := new(big.Float).Mul(unitPrice, new(big.Float).SetInt64(int64(effQty)))
	naturalTotal.Add(naturalTotal, fixedCost)

	// Check higher price breaks to see if ordering more reduces the total cost.
	// Cap: never order more than 3× the actual cart quantity to avoid excessive over-ordering.
	optimalQty := effQty
	optimalUnitPriceStr := unitPriceStr
	optimalTotal := naturalTotal
	cap := cartQty * 100
	for _, pb := range breaks {
		if pb.BreakQty <= effQty || pb.BreakQty > cap {
			continue
		}
		breakPrice, ok := new(big.Float).SetString(pb.UnitPrice)
		if !ok {
			continue
		}
		candidateTotal := new(big.Float).Mul(breakPrice, new(big.Float).SetInt64(int64(pb.BreakQty)))
		candidateTotal.Add(candidateTotal, fixedCost)
		if candidateTotal.Cmp(optimalTotal) < 0 {
			optimalTotal = candidateTotal
			optimalQty = pb.BreakQty
			optimalUnitPriceStr = pb.UnitPrice
		}
	}

	// Build effective unit price at the optimal quantity.
	var unitPriceN pgtype.Numeric
	if optimalUnitPriceStr != "" {
		_ = unitPriceN.Scan(optimalUnitPriceStr)
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
		LastRefreshedAt: l.LastRefreshedAt,
		OptimizedQty:    optimalQty,
	}
	return candidate, optimalTotal
}

// listingStockStatus decides whether a listing may satisfy a distributor preference for effQty
// units. Only fresh data that confirms insufficient stock disqualifies a listing; missing or
// stale stock is honoured optimistically and flagged (stale=true) so the UI can warn.
func listingStockStatus(l dbgen.GetListingsForCartResolutionRow, effQty int32, now time.Time) (qualifies, stale bool) {
	fresh := false
	if l.LastRefreshedAt.Valid && l.CacheDurationSeconds > 0 {
		fresh = now.Sub(l.LastRefreshedAt.Time) <= time.Duration(l.CacheDurationSeconds)*time.Second
	}
	if l.StockAvailable == nil || !fresh {
		return true, true // unknown or stale stock: optimistic, flagged
	}
	if *l.StockAvailable >= effQty {
		return true, false
	}
	return false, false // fresh data confirms insufficient stock
}

// pickCheapest returns the index of the lowest-total eval, tie-broken by preferred then name.
func pickCheapest(evals []listingEval) int {
	best := -1
	for i := range evals {
		if best == -1 {
			best = i
			continue
		}
		cmp := evals[i].total.Cmp(evals[best].total)
		switch {
		case cmp < 0:
			best = i
		case cmp == 0 && evals[i].candidate.Preferred && !evals[best].candidate.Preferred:
			best = i
		case cmp == 0 && evals[i].candidate.Preferred == evals[best].candidate.Preferred &&
			evals[i].candidate.DistributorName < evals[best].candidate.DistributorName:
			best = i
		}
	}
	return best
}

// withinTolerance reports whether honouring the preferred pick (prefTotal) is acceptable versus
// the cheapest alternative (cheapestTotal). A preferred pick that is no more expensive is always
// allowed. When guardrails are set the pick must fall within the percent OR absolute bound; with
// no guardrail configured the preference is honoured at any price.
func withinTolerance(prefTotal, cheapestTotal, pct, abs *big.Float) bool {
	if prefTotal.Cmp(cheapestTotal) <= 0 {
		return true
	}
	if pct == nil && abs == nil {
		return true
	}
	if pct != nil {
		factor := new(big.Float).Add(big.NewFloat(1), new(big.Float).Quo(pct, big.NewFloat(100)))
		limit := new(big.Float).Mul(cheapestTotal, factor)
		if prefTotal.Cmp(limit) <= 0 {
			return true
		}
	}
	if abs != nil {
		limit := new(big.Float).Add(cheapestTotal, abs)
		if prefTotal.Cmp(limit) <= 0 {
			return true
		}
	}
	return false
}

// pickBestListing selects a listing for a cart item according to the team's sourcing policy.
//
// Under the "cost" strategy (the default) it returns the lowest-total-cost listing, exactly as
// before. Under "preferred_in_stock" it walks the team's distributor ranking and picks the
// cheapest listing from the highest-ranked distributor that can supply the quantity in stock,
// falling through the ranking when a distributor is out of stock and finally to pure cost when no
// ranked distributor qualifies. A cost guardrail can veto an over-expensive preferred pick.
// The chosen listing's Reason (and PreferredStale) records why it was selected.
// Returns nil if no listing has a usable price.
func pickBestListing(listings []dbgen.GetListingsForCartResolutionRow, cartQty int32, policy sourcingPolicy, now time.Time) *resolvedListing {
	evals := make([]listingEval, 0, len(listings))
	anyRanked := false
	var minRankedPriority int32
	haveMinRanked := false
	for _, l := range listings {
		cand, total := evalListing(l, cartQty)
		if cand == nil {
			continue
		}
		qualifies, stale := listingStockStatus(l, max(cartQty, l.Moq), now)
		evals = append(evals, listingEval{
			candidate: cand,
			total:     total,
			priority:  l.Priority,
			qualifies: qualifies,
			stale:     stale,
		})
		if l.Priority != nil {
			anyRanked = true
			if !haveMinRanked || *l.Priority < minRankedPriority {
				minRankedPriority = *l.Priority
				haveMinRanked = true
			}
		}
	}
	if len(evals) == 0 {
		return nil
	}

	cheapest := evals[pickCheapest(evals)]

	// Cost strategy, or no ranking applies to this part's distributors → lowest cost.
	if policy.strategy != "preferred_in_stock" || !anyRanked {
		best := cheapest.candidate
		if len(evals) == 1 {
			best.Reason = "only_option"
		} else {
			best.Reason = "cost"
		}
		return best
	}

	// preferred_in_stock: cheapest qualifying listing from the highest available rank tier.
	prefIdx := -1
	for i := range evals {
		e := evals[i]
		if e.priority == nil || !e.qualifies {
			continue
		}
		if prefIdx == -1 {
			prefIdx = i
			continue
		}
		cur := evals[prefIdx]
		if *e.priority < *cur.priority || (*e.priority == *cur.priority && e.total.Cmp(cur.total) < 0) {
			prefIdx = i
		}
	}

	if prefIdx == -1 {
		// Every ranked distributor is out of stock for this part → fall back to cost.
		best := cheapest.candidate
		best.Reason = "out_of_stock_fallback"
		return best
	}

	pref := evals[prefIdx]

	// Cost guardrail: don't overpay for the preference beyond the configured tolerance.
	if !withinTolerance(pref.total, cheapest.total, policy.tolerancePct, policy.toleranceAbs) {
		best := cheapest.candidate
		best.Reason = "over_tolerance"
		return best
	}

	best := pref.candidate
	best.PreferredStale = pref.stale
	if pref.priority != nil && *pref.priority == minRankedPriority {
		best.Reason = "preferred"
	} else {
		best.Reason = "preferred_fallback"
	}
	return best
}

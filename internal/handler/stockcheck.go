package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/substitute"
	"github.com/Yamakaja/partrack/internal/timing"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// StockCheckItem is the per-line-item result of a stock check.
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

type subInfo struct {
	PartID   string
	PartMPN  *string
	Priority int32
}

// computeStockCheck fetches data and computes stock check results for a BOM revision.
// It also returns the computed fully_buildable value.
// q may be a transaction-scoped Queries or the global one.
func (h *Handler) computeStockCheck(
	ctx context.Context,
	q *dbgen.Queries,
	teamID pgtype.UUID,
	revID pgtype.UUID,
	buildQty int32,
) (items []StockCheckItem, fullyBuildable int32, err error) {
	type lineItemsResult struct {
		items []dbgen.ListBOMLineItemsRow
		err   error
	}
	type inventoryResult struct {
		rows []dbgen.GetInventoryQuantitiesForRevisionRow
		err  error
	}
	type subsResult struct {
		rows []dbgen.ListAllSubstitutesForRevisionRow
		err  error
	}

	liCh := make(chan lineItemsResult, 1)
	invCh := make(chan inventoryResult, 1)
	subsCh := make(chan subsResult, 1)

	go func() {
		it, e := q.ListBOMLineItems(ctx, revID)
		liCh <- lineItemsResult{it, e}
	}()
	go func() {
		rows, e := q.GetInventoryQuantitiesForRevision(ctx,
			dbgen.GetInventoryQuantitiesForRevisionParams{
				BomRevisionID: revID,
				TeamID:        teamID,
			})
		invCh <- inventoryResult{rows, e}
	}()
	go func() {
		rows, e := q.ListAllSubstitutesForRevision(ctx, revID)
		subsCh <- subsResult{rows, e}
	}()

	liRes := <-liCh
	invRes := <-invCh
	subsRes := <-subsCh

	if liRes.err != nil || invRes.err != nil || subsRes.err != nil {
		return nil, 0, errors.New("query error")
	}

	// Collect all part IDs (primary + substitute) for the second-wave queries.
	partIDSet := make(map[string]pgtype.UUID)
	for _, li := range liRes.items {
		if li.ResolvedPartID.Valid {
			partIDSet[li.ResolvedPartID.String()] = li.ResolvedPartID
		}
	}
	for _, s := range subsRes.rows {
		partIDSet[s.PartID.String()] = s.PartID
	}
	partIDs := make([]pgtype.UUID, 0, len(partIDSet))
	for _, pid := range partIDSet {
		partIDs = append(partIDs, pid)
	}

	type onOrderResult struct {
		rows []dbgen.GetOnOrderQtysForPartsRow
		err  error
	}
	type reservedResult struct {
		rows []dbgen.GetReservedQtysForPartsRow
		err  error
	}

	onOrderCh := make(chan onOrderResult, 1)
	reservedCh := make(chan reservedResult, 1)

	go func() {
		if len(partIDs) == 0 {
			onOrderCh <- onOrderResult{}
			return
		}
		rows, e := q.GetOnOrderQtysForParts(ctx, dbgen.GetOnOrderQtysForPartsParams{
			PartIds: partIDs,
			TeamID:  teamID,
		})
		onOrderCh <- onOrderResult{rows, e}
	}()
	go func() {
		if len(partIDs) == 0 {
			reservedCh <- reservedResult{}
			return
		}
		rows, e := q.GetReservedQtysForParts(ctx, dbgen.GetReservedQtysForPartsParams{
			PartIds: partIDs,
			TeamID:  teamID,
		})
		reservedCh <- reservedResult{rows, e}
	}()

	onOrderRes := <-onOrderCh
	reservedRes := <-reservedCh

	if onOrderRes.err != nil || reservedRes.err != nil {
		return nil, 0, errors.New("query error")
	}

	invByPart := make(map[string]dbgen.GetInventoryQuantitiesForRevisionRow)
	for _, row := range invRes.rows {
		invByPart[row.PartID.String()] = row
	}

	onOrderByPart := make(map[string]int32)
	for _, row := range onOrderRes.rows {
		onOrderByPart[row.PartID.String()] = row.QuantityOnOrder
	}

	reservedByPart := make(map[string]int32)
	for _, row := range reservedRes.rows {
		reservedByPart[row.PartID.String()] = row.TotalReserved
	}

	onHandByPart := make(map[string]int32, len(invByPart))
	for pid, row := range invByPart {
		onHandByPart[pid] = row.QuantityOnHand
	}

	// Build priority-ordered substitute list per line item.
	// subsRes.rows is already ordered by (bom_line_item_id, priority ASC).
	subsByItem := make(map[string][]subInfo)
	subIDsByItem := make(map[string][]string)
	for _, s := range subsRes.rows {
		key := s.BomLineItemID.String()
		subsByItem[key] = append(subsByItem[key], subInfo{
			PartID:   s.PartID.String(),
			PartMPN:  s.PartMpn,
			Priority: s.Priority,
		})
		subIDsByItem[key] = append(subIDsByItem[key], s.PartID.String())
	}

	// Choose substitutes via the shared rule so this preview matches what a
	// build will persist (internal/substitute, also used by the selection job).
	selItems := make([]substitute.Item, 0, len(liRes.items))
	for _, li := range liRes.items {
		if !li.ResolvedPartID.Valid {
			continue
		}
		selItems = append(selItems, substitute.Item{
			LineItemID: li.ID.String(),
			PrimaryID:  li.ResolvedPartID.String(),
			Required:   li.Quantity * buildQty,
		})
	}
	selectedByItem := substitute.Select(selItems, subIDsByItem, onHandByPart, reservedByPart)

	items = make([]StockCheckItem, len(liRes.items))
	hasResolved := false

	for i, li := range liRes.items {
		itemID := li.ID.String()
		required := li.Quantity * buildQty

		var stockOnHand, quantityReserved, quantityFree, quantityOnOrder int32
		var minStockQty *int32
		var stockStatus string
		var shortfall int32
		var selectedSubID *string
		var selectedSubMPN *string
		var subQtyOnHand *int32

		if !li.ResolvedPartID.Valid {
			stockStatus = "out_of_stock"
			shortfall = required
		} else {
			pid := li.ResolvedPartID.String()
			inv, hasInv := invByPart[pid]
			if hasInv {
				stockOnHand = inv.QuantityOnHand
				minStockQty = inv.MinimumStockQuantity
			}
			quantityReserved = reservedByPart[pid]
			quantityFree = stockOnHand - quantityReserved
			if quantityFree < 0 {
				quantityFree = 0
			}
			quantityOnOrder = onOrderByPart[pid]

			// effectiveFree is the free stock of the part that will actually fulfil
			// this line item — the primary, or the substitute chosen by the shared
			// rule when the primary is insufficient.
			effectiveFree := quantityFree
			if subPartID, ok := selectedByItem[itemID]; ok {
				subFree := substitute.FreeStock(onHandByPart[subPartID], reservedByPart[subPartID])
				effectiveFree = subFree
				qoh := onHandByPart[subPartID]
				selectedSubID = &subPartID
				subQtyOnHand = &qoh
				for _, sub := range subsByItem[itemID] {
					if sub.PartID == subPartID {
						selectedSubMPN = sub.PartMPN
						break
					}
				}
			}

			if effectiveFree >= required {
				stockStatus = "in_stock"
			} else if effectiveFree > 0 || stockOnHand > 0 {
				stockStatus = "partial"
				shortfall = required - stockOnHand
				if shortfall < 0 {
					shortfall = 0
				}
			} else {
				stockStatus = "out_of_stock"
				shortfall = required - stockOnHand
				if shortfall < 0 {
					shortfall = 0
				}
			}

			// fully_buildable reflects the free stock of the effective part, so
			// stock already reserved by other builds is not double-counted.
			if li.Quantity > 0 {
				maxBoards := effectiveFree / li.Quantity
				if !hasResolved || maxBoards < fullyBuildable {
					fullyBuildable = maxBoards
					hasResolved = true
				}
			}
		}

		postBuildStock := stockOnHand - required
		belowRestock := minStockQty != nil && postBuildStock < *minStockQty

		items[i] = StockCheckItem{
			BomLineItemID:         itemID,
			ReferenceDesignators:  li.ReferenceDesignators,
			QuantityPerBoard:      li.Quantity,
			RequiredQuantity:      required,
			QuantityOnHand:        stockOnHand,
			QuantityReserved:      quantityReserved,
			QuantityFree:          quantityFree,
			QuantityOnOrder:       quantityOnOrder,
			Shortfall:             shortfall,
			StockStatus:           stockStatus,
			ResolvedPartID:        uuidPtrString(li.ResolvedPartID),
			ResolvedPartMpn:       li.ResolvedPartMpn,
			MinStockQty:           minStockQty,
			BelowRestock:          belowRestock,
			SelectedSubstituteID:  selectedSubID,
			SelectedSubstituteMPN: selectedSubMPN,
			SubstituteQtyOnHand:   subQtyOnHand,
		}
	}

	return items, fullyBuildable, nil
}

// StockCheck handles GET /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/stock-check
// Query: build_quantity (required). The build_quantity is ephemeral — not stored.
// Returns per-line-item status for the specified BOM revision.
func (h *Handler) StockCheck(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	if !h.requireRevisionInTeam(w, r, revID, teamID) {
		return
	}

	buildQtyStr := r.URL.Query().Get("build_quantity")
	if buildQtyStr == "" {
		writeError(w, http.StatusBadRequest, "build_quantity is required", "BAD_REQUEST")
		return
	}
	buildQty64, err := strconv.ParseInt(buildQtyStr, 10, 32)
	if err != nil || buildQty64 <= 0 {
		writeError(w, http.StatusBadRequest, "build_quantity must be a positive integer", "BAD_REQUEST")
		return
	}
	buildQty := int32(buildQty64)

	_, err = h.Queries.GetRevision(r.Context(), dbgen.GetRevisionParams{
		ID:        revID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "revision not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if sp := timing.FromContext(r.Context()); sp != nil {
		s := sp.Start("stock_compute", "stock status computation")
		defer s.Stop()
	}

	items, fullyBuildable, err := h.computeStockCheck(r.Context(), h.Queries, teamID, revID, buildQty)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"revision_id":     revID.String(),
		"build_quantity":  buildQty,
		"fully_buildable": fullyBuildable,
		"items":           items,
	})
}

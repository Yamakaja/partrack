package handler

import (
	"errors"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/timing"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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

	type revResult struct {
		err error
	}
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
	type onOrderResult struct {
		rows []dbgen.GetOnOrderQtysForPartsRow
		err  error
	}
	type reservedResult struct {
		rows []dbgen.GetReservedQtysForPartsRow
		err  error
	}

	revCh := make(chan revResult, 1)
	liCh := make(chan lineItemsResult, 1)
	invCh := make(chan inventoryResult, 1)
	subsCh := make(chan subsResult, 1)
	// on-order and reserved are fetched after we know the part IDs; run in a second wave.

	go func() {
		_, err := h.Queries.GetRevision(r.Context(), dbgen.GetRevisionParams{
			ID:        revID,
			ProjectID: projectID,
		})
		revCh <- revResult{err}
	}()
	go func() {
		items, err := h.Queries.ListBOMLineItems(r.Context(), revID)
		liCh <- lineItemsResult{items, err}
	}()
	go func() {
		rows, err := h.Queries.GetInventoryQuantitiesForRevision(r.Context(),
			dbgen.GetInventoryQuantitiesForRevisionParams{
				BomRevisionID: revID,
				TeamID:        teamID,
			})
		invCh <- inventoryResult{rows, err}
	}()
	go func() {
		rows, err := h.Queries.ListAllSubstitutesForRevision(r.Context(), revID)
		subsCh <- subsResult{rows, err}
	}()

	revRes := <-revCh
	liRes := <-liCh
	invRes := <-invCh
	subsRes := <-subsCh

	if revRes.err != nil {
		if errors.Is(revRes.err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "revision not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if liRes.err != nil || invRes.err != nil || subsRes.err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Collect resolved part IDs for the second-wave queries.
	partIDSet := make(map[string]pgtype.UUID)
	for _, li := range liRes.items {
		if li.ResolvedPartID.Valid {
			partIDSet[li.ResolvedPartID.String()] = li.ResolvedPartID
		}
	}
	partIDs := make([]pgtype.UUID, 0, len(partIDSet))
	for _, pid := range partIDSet {
		partIDs = append(partIDs, pid)
	}

	onOrderCh := make(chan onOrderResult, 1)
	reservedCh := make(chan reservedResult, 1)

	go func() {
		if len(partIDs) == 0 {
			onOrderCh <- onOrderResult{}
			return
		}
		rows, err := h.Queries.GetOnOrderQtysForParts(r.Context(), dbgen.GetOnOrderQtysForPartsParams{
			PartIds: partIDs,
			TeamID:  teamID,
		})
		onOrderCh <- onOrderResult{rows, err}
	}()
	go func() {
		if len(partIDs) == 0 {
			reservedCh <- reservedResult{}
			return
		}
		rows, err := h.Queries.GetReservedQtysForParts(r.Context(), dbgen.GetReservedQtysForPartsParams{
			PartIds: partIDs,
			TeamID:  teamID,
		})
		reservedCh <- reservedResult{rows, err}
	}()

	onOrderRes := <-onOrderCh
	reservedRes := <-reservedCh

	if onOrderRes.err != nil || reservedRes.err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Build lookup maps and compute per-item stock status.
	if sp := timing.FromContext(r.Context()); sp != nil {
		s := sp.Start("stock_compute", "stock status computation")
		defer s.Stop()
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

	subStockByItem := make(map[string]int32)
	for _, s := range subsRes.rows {
		key := s.BomLineItemID.String()
		pid := s.PartID.String()
		var subStock int32
		if inv, ok := invByPart[pid]; ok {
			subStock = inv.QuantityOnHand
		}
		if existing, seen := subStockByItem[key]; !seen || subStock > existing {
			subStockByItem[key] = subStock
		}
	}

	type itemResult struct {
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
	}

	results := make([]itemResult, len(liRes.items))
	var fullyBuildable int32
	hasResolved := false

	for i, li := range liRes.items {
		itemID := li.ID.String()
		required := li.Quantity * buildQty

		var stockOnHand, quantityReserved, quantityFree, quantityOnOrder int32
		var minStockQty *int32
		var stockStatus string
		var shortfall int32

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

			effectiveStock := stockOnHand
			if effectiveStock < required {
				if subStock, ok := subStockByItem[itemID]; ok && subStock > effectiveStock {
					effectiveStock = subStock
				}
			}

			if effectiveStock >= required {
				stockStatus = "in_stock"
			} else if effectiveStock > 0 {
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

			if li.Quantity > 0 {
				maxBoards := stockOnHand / li.Quantity
				if !hasResolved || maxBoards < fullyBuildable {
					fullyBuildable = maxBoards
					hasResolved = true
				}
			}
		}

		postBuildStock := stockOnHand - required
		belowRestock := minStockQty != nil && postBuildStock < *minStockQty

		results[i] = itemResult{
			BomLineItemID:        itemID,
			ReferenceDesignators: li.ReferenceDesignators,
			QuantityPerBoard:     li.Quantity,
			RequiredQuantity:     required,
			QuantityOnHand:       stockOnHand,
			QuantityReserved:     quantityReserved,
			QuantityFree:         quantityFree,
			QuantityOnOrder:      quantityOnOrder,
			Shortfall:            shortfall,
			StockStatus:          stockStatus,
			ResolvedPartID:       uuidPtrString(li.ResolvedPartID),
			ResolvedPartMpn:      li.ResolvedPartMpn,
			MinStockQty:          minStockQty,
			BelowRestock:         belowRestock,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"revision_id":     revID.String(),
		"build_quantity":  buildQty,
		"fully_buildable": fullyBuildable,
		"items":           results,
	})
}

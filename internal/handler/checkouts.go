package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListCheckouts handles GET /api/v1/teams/{teamId}/checkouts
// Query: limit, offset
func (h *Handler) ListCheckouts(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountCheckouts(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListCheckouts(r.Context(), dbgen.ListCheckoutsParams{
		TeamID: teamID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type checkoutItem struct {
		ID                     string  `json:"id"`
		PerformedAt            string  `json:"performed_at"`
		Reason                 *string `json:"reason"`
		PerformedByDisplayName string  `json:"performed_by_display_name"`
		ItemCount              int64   `json:"item_count"`
	}

	items := make([]checkoutItem, len(rows))
	for i, c := range rows {
		items[i] = checkoutItem{
			ID:                     c.ID.String(),
			PerformedAt:            c.PerformedAt.Time.UTC().Format(time.RFC3339),
			Reason:                 c.Reason,
			PerformedByDisplayName: c.PerformedByDisplayName,
			ItemCount:              c.ItemCount,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// CreateCheckout handles POST /api/v1/teams/{teamId}/checkouts
// Body: {reason?, notes?, items: [{inventory_entry_id, quantity, reconcile_to?}]}
// Deducts stock and creates adjustment records. Returns 409 with insufficient_items
// when stock would go negative and reconcile_to is not provided.
func (h *Handler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var body struct {
		Reason *string `json:"reason"`
		Notes  *string `json:"notes"` // alias for reason
		Items  []struct {
			InventoryEntryID string  `json:"inventory_entry_id"`
			PartID           string  `json:"part_id"` // fallback if no inventory_entry_id
			Quantity         int32   `json:"quantity"`
			ReconcileTo      *int32  `json:"reconcile_to"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if len(body.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required and must not be empty", "BAD_REQUEST")
		return
	}

	// Accept either reason or notes field.
	reason := body.Reason
	if reason == nil {
		reason = body.Notes
	}

	// Pre-resolve inventory entries before opening the transaction so we can
	// return a clean 409 without being inside a tx.
	type resolvedItem struct {
		InventoryEntryID pgtype.UUID
		PartID           pgtype.UUID
		PartMpn          *string
		Quantity         int32
		ReconcileTo      *int32
		HasInventory     bool
	}

	resolved := make([]resolvedItem, 0, len(body.Items))
	for _, item := range body.Items {
		if item.Quantity <= 0 {
			writeError(w, http.StatusBadRequest, "item quantity must be positive", "BAD_REQUEST")
			return
		}

		ri := resolvedItem{Quantity: item.Quantity, ReconcileTo: item.ReconcileTo}

		if item.InventoryEntryID != "" {
			var entryID pgtype.UUID
			if err := entryID.Scan(item.InventoryEntryID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid inventory_entry_id: "+item.InventoryEntryID, "BAD_REQUEST")
				return
			}
			entry, err := h.Queries.GetInventoryEntryForUpdate(r.Context(), entryID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if err == nil {
				ri.InventoryEntryID = entry.ID
				ri.PartID = entry.PartID
				ri.HasInventory = true
			}
		} else if item.PartID != "" {
			var partID pgtype.UUID
			if err := partID.Scan(item.PartID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid part_id: "+item.PartID, "BAD_REQUEST")
				return
			}
			ri.PartID = partID
			inv, err := h.Queries.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
				TeamID: teamID,
				PartID: partID,
			})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if err == nil {
				ri.InventoryEntryID = inv.ID
				ri.HasInventory = true
			}
		}

		resolved = append(resolved, ri)
	}

	// Stock check pass — identify items that need reconciliation.
	type insufficientItem struct {
		InventoryEntryID string  `json:"inventory_entry_id"`
		PartID           string  `json:"part_id"`
		PartMpn          *string `json:"part_mpn"`
		QuantityOnHand   int32   `json:"quantity_on_hand"`
		QuantityRequested int32  `json:"quantity_requested"`
	}
	var insufficientItems []insufficientItem

	// We need current stock for the pre-check. Fetch outside the tx.
	type stockInfo struct {
		quantityOnHand int32
	}
	stockMap := make(map[string]stockInfo)
	for i := range resolved {
		ri := &resolved[i]
		if !ri.HasInventory {
			continue
		}
		entry, err := h.Queries.GetInventoryEntryForUpdate(r.Context(), ri.InventoryEntryID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		stockMap[ri.InventoryEntryID.String()] = stockInfo{quantityOnHand: entry.QuantityOnHand}
	}

	for _, ri := range resolved {
		if !ri.HasInventory {
			continue
		}
		si := stockMap[ri.InventoryEntryID.String()]
		if si.quantityOnHand-ri.Quantity < 0 {
			if ri.ReconcileTo == nil {
				insufficientItems = append(insufficientItems, insufficientItem{
					InventoryEntryID:  ri.InventoryEntryID.String(),
					PartID:            ri.PartID.String(),
					PartMpn:           ri.PartMpn,
					QuantityOnHand:    si.quantityOnHand,
					QuantityRequested: ri.Quantity,
				})
			} else if *ri.ReconcileTo < ri.Quantity {
				writeError(w, http.StatusBadRequest,
					"reconcile_to must be >= quantity requested", "BAD_REQUEST")
				return
			}
		}
	}

	if len(insufficientItems) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":              "some items have insufficient stock; provide reconcile_to to correct",
			"code":               "INSUFFICIENT_STOCK",
			"insufficient_items": insufficientItems,
		})
		return
	}

	// Compute reservation warnings (non-blocking): items where the checkout quantity
	// exceeds quantity_free (on_hand - reserved_by_active_builds).
	type reservationWarning struct {
		PartID           string `json:"part_id"`
		QuantityOnHand   int32  `json:"quantity_on_hand"`
		QuantityReserved int32  `json:"quantity_reserved"`
		QuantityFree     int32  `json:"quantity_free"`
		CheckoutQuantity int32  `json:"checkout_quantity"`
	}
	var reservationWarnings []reservationWarning
	{
		partIDs := make([]pgtype.UUID, 0, len(resolved))
		for _, ri := range resolved {
			if ri.HasInventory {
				partIDs = append(partIDs, ri.PartID)
			}
		}
		if len(partIDs) > 0 {
			reservedRows, err := h.Queries.GetReservedQtysForParts(r.Context(), dbgen.GetReservedQtysForPartsParams{
				PartIds: partIDs,
				TeamID:  teamID,
			})
			if err == nil {
				reservedByPart := make(map[string]int32, len(reservedRows))
				for _, row := range reservedRows {
					reservedByPart[row.PartID.String()] = row.TotalReserved
				}
				for _, ri := range resolved {
					if !ri.HasInventory {
						continue
					}
					si := stockMap[ri.InventoryEntryID.String()]
					reserved := reservedByPart[ri.PartID.String()]
					free := si.quantityOnHand - reserved
					if free < 0 {
						free = 0
					}
					if ri.Quantity > free {
						reservationWarnings = append(reservationWarnings, reservationWarning{
							PartID:           ri.PartID.String(),
							QuantityOnHand:   si.quantityOnHand,
							QuantityReserved: reserved,
							QuantityFree:     free,
							CheckoutQuantity: ri.Quantity,
						})
					}
				}
			}
		}
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	checkout, err := qtx.CreateCheckout(r.Context(), dbgen.CreateCheckoutParams{
		TeamID:      teamID,
		Reason:      reason,
		PerformedBy: userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type lineItemResult struct {
		PartID   string `json:"part_id"`
		Quantity int32  `json:"quantity"`
		Adjusted bool   `json:"adjusted"`
	}

	var lineItemResults []lineItemResult

	for _, ri := range resolved {
		if _, err := qtx.CreateCheckoutLineItem(r.Context(), dbgen.CreateCheckoutLineItemParams{
			GeneralCheckoutID: checkout.ID,
			PartID:            ri.PartID,
			Quantity:          ri.Quantity,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		adjusted := false
		if ri.HasInventory {
			// Lock the row inside the transaction.
			locked, err := qtx.GetInventoryEntryForUpdate(r.Context(), ri.InventoryEntryID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}

			// Apply reconciliation if needed.
			if ri.ReconcileTo != nil && locked.QuantityOnHand < *ri.ReconcileTo {
				reconcileDelta := *ri.ReconcileTo - locked.QuantityOnHand
				reconcileReason := "Stock reconciliation: observed count corrected before checkout"
				if _, err := qtx.CreateStockAdjustment(r.Context(), dbgen.CreateStockAdjustmentParams{
					InventoryEntryID: ri.InventoryEntryID,
					Delta:            reconcileDelta,
					Reason:           &reconcileReason,
					CreatedBy:        userID,
				}); err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
				if _, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
					ID:             ri.InventoryEntryID,
					QuantityOnHand: reconcileDelta,
				}); err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
			}

			delta := -ri.Quantity
			if _, err := qtx.CreateCheckoutStockAdjustment(r.Context(), dbgen.CreateCheckoutStockAdjustmentParams{
				InventoryEntryID:  ri.InventoryEntryID,
				Delta:             int32(delta),
				Reason:            reason,
				CreatedBy:         userID,
				GeneralCheckoutID: checkout.ID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if _, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
				ID:             ri.InventoryEntryID,
				QuantityOnHand: int32(delta),
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			adjusted = true
		}

		lineItemResults = append(lineItemResults, lineItemResult{
			PartID:   ri.PartID.String(),
			Quantity: ri.Quantity,
			Adjusted: adjusted,
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "checkout_changed", teamID.String(), map[string]string{"checkout_id": checkout.ID.String()})
	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{})

	if reservationWarnings == nil {
		reservationWarnings = []reservationWarning{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                   checkout.ID.String(),
		"performed_at":         checkout.PerformedAt.Time.UTC().Format(time.RFC3339),
		"reason":               checkout.Reason,
		"items":                lineItemResults,
		"reservation_warnings": reservationWarnings,
	})
}

// GetCheckout handles GET /api/v1/teams/{teamId}/checkouts/{checkoutId}
func (h *Handler) GetCheckout(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var checkoutID pgtype.UUID
	if err := checkoutID.Scan(chi.URLParam(r, "checkoutId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid checkout id", "BAD_REQUEST")
		return
	}

	checkout, err := h.Queries.GetCheckout(r.Context(), dbgen.GetCheckoutParams{
		ID:     checkoutID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "checkout not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	lineItems, err := h.Queries.GetCheckoutLineItems(r.Context(), dbgen.GetCheckoutLineItemsParams{
		GeneralCheckoutID: checkoutID,
		TeamID:            teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type lineItemResp struct {
		PartID            string  `json:"part_id"`
		PartMpn           *string `json:"part_mpn"`
		Quantity          int32   `json:"quantity"`
		StockAdjustmentID *string `json:"stock_adjustment_id"`
	}

	items := make([]lineItemResp, len(lineItems))
	for i, li := range lineItems {
		items[i] = lineItemResp{
			PartID:            li.PartID.String(),
			PartMpn:           li.PartMpn,
			Quantity:          li.Quantity,
			StockAdjustmentID: uuidPtrString(li.StockAdjustmentID),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                       checkout.ID.String(),
		"performed_at":             checkout.PerformedAt.Time.UTC().Format(time.RFC3339),
		"reason":                   checkout.Reason,
		"performed_by_display_name": checkout.PerformedByDisplayName,
		"items":                    items,
	})
}

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
// Body: {reason?, items: [{part_id, quantity}]}
// Deducts stock and creates adjustment records.
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
		Items  []struct {
			PartID   string `json:"part_id"`
			Quantity int32  `json:"quantity"`
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

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	checkout, err := qtx.CreateCheckout(r.Context(), dbgen.CreateCheckoutParams{
		TeamID:      teamID,
		Reason:      body.Reason,
		PerformedBy: userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type lineItemResult struct {
		PartID  string  `json:"part_id"`
		PartMpn *string `json:"part_mpn,omitempty"`
		Quantity int32  `json:"quantity"`
		Adjusted bool   `json:"adjusted"`
	}

	var lineItemResults []lineItemResult

	for _, item := range body.Items {
		if item.Quantity <= 0 {
			writeError(w, http.StatusBadRequest, "item quantity must be positive", "BAD_REQUEST")
			return
		}
		var partID pgtype.UUID
		if err := partID.Scan(item.PartID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid part_id: "+item.PartID, "BAD_REQUEST")
			return
		}

		if _, err := qtx.CreateCheckoutLineItem(r.Context(), dbgen.CreateCheckoutLineItemParams{
			GeneralCheckoutID: checkout.ID,
			PartID:            partID,
			Quantity:          item.Quantity,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		// Try to find and deduct from inventory entry.
		invEntry, err := qtx.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
			TeamID: teamID,
			PartID: partID,
		})
		adjusted := false
		if err == nil {
			delta := -item.Quantity
			if _, err := qtx.CreateCheckoutStockAdjustment(r.Context(), dbgen.CreateCheckoutStockAdjustmentParams{
				InventoryEntryID:  invEntry.ID,
				Delta:             int32(delta),
				Reason:            body.Reason,
				CreatedBy:         userID,
				GeneralCheckoutID: checkout.ID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if _, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
				ID:             invEntry.ID,
				QuantityOnHand: int32(delta),
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			adjusted = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		lineItemResults = append(lineItemResults, lineItemResult{
			PartID:   partID.String(),
			Quantity: item.Quantity,
			Adjusted: adjusted,
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                       checkout.ID.String(),
		"performed_at":             checkout.PerformedAt.Time.UTC().Format(time.RFC3339),
		"reason":                   checkout.Reason,
		"items":                    lineItemResults,
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

package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func orderResponse(o dbgen.GetOrderRow) map[string]any {
	resp := map[string]any{
		"id":                     o.ID.String(),
		"distributor_id":         o.DistributorID.String(),
		"distributor_name":       o.DistributorName,
		"status":                 string(o.Status),
		"external_reference":     o.ExternalReference,
		"notes":                  o.Notes,
		"created_at":             o.CreatedAt.Time.UTC().Format(time.RFC3339),
		"created_by_display_name": o.CreatedByDisplayName,
	}
	if o.SubmissionMethod.Valid {
		resp["submission_method"] = string(o.SubmissionMethod.OrderSubmissionMethod)
	} else {
		resp["submission_method"] = nil
	}
	if o.ExternalRefLinkedAt.Valid {
		resp["external_ref_linked_at"] = o.ExternalRefLinkedAt.Time.UTC().Format(time.RFC3339)
	} else {
		resp["external_ref_linked_at"] = nil
	}
	if o.ReceivedAt.Valid {
		resp["received_at"] = o.ReceivedAt.Time.UTC().Format(time.RFC3339)
	} else {
		resp["received_at"] = nil
	}
	return resp
}

func orderLineItemResponse(li dbgen.GetOrderLineItemsRow) map[string]any {
	return map[string]any{
		"id":                       li.ID.String(),
		"listing_id":               li.ListingID.String(),
		"part_mpn":                 li.PartMpn,
		"sku":                      li.Sku,
		"packaging_type":           li.PackagingType,
		"quantity_ordered":         li.QuantityOrdered,
		"quantity_received":        li.QuantityReceived,
		"unit_price_snapshot":      numericString(li.UnitPriceSnapshot),
		"fixed_order_cost_snapshot": numericString(li.FixedOrderCostSnapshot),
	}
}

// ListOrders handles GET /api/v1/teams/{teamId}/orders
// Query: status?, distributor_id?, limit, offset
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var statusFilter dbgen.NullOrderStatus
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = dbgen.NullOrderStatus{
			OrderStatus: dbgen.OrderStatus(s),
			Valid:        true,
		}
	}

	var distributorID pgtype.UUID
	if s := r.URL.Query().Get("distributor_id"); s != "" {
		if err := distributorID.Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid distributor_id", "BAD_REQUEST")
			return
		}
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountOrders(r.Context(), dbgen.CountOrdersParams{
		TeamID:        teamID,
		Status:        statusFilter,
		DistributorID: distributorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListOrders(r.Context(), dbgen.ListOrdersParams{
		TeamID:        teamID,
		Status:        statusFilter,
		DistributorID: distributorID,
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type orderItem struct {
		ID                string  `json:"id"`
		DistributorID     string  `json:"distributor_id"`
		DistributorName   string  `json:"distributor_name"`
		Status            string  `json:"status"`
		ExternalReference *string `json:"external_reference"`
		CreatedAt         string  `json:"created_at"`
		ItemCount         int64   `json:"item_count"`
	}

	items := make([]orderItem, len(rows))
	for i, o := range rows {
		items[i] = orderItem{
			ID:                o.ID.String(),
			DistributorID:     o.DistributorID.String(),
			DistributorName:   o.DistributorName,
			Status:            string(o.Status),
			ExternalReference: o.ExternalReference,
			CreatedAt:         o.CreatedAt.Time.UTC().Format(time.RFC3339),
			ItemCount:         o.ItemCount,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetOrder handles GET /api/v1/teams/{teamId}/orders/{orderId}
func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(chi.URLParam(r, "orderId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}

	order, err := h.Queries.GetOrder(r.Context(), dbgen.GetOrderParams{
		ID:     orderID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	lineItems, err := h.Queries.GetOrderLineItems(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	resp := orderResponse(order)
	items := make([]map[string]any, len(lineItems))
	for i, li := range lineItems {
		items[i] = orderLineItemResponse(li)
	}
	resp["items"] = items

	writeJSON(w, http.StatusOK, resp)
}

// UpdateOrder handles PATCH /api/v1/teams/{teamId}/orders/{orderId}
// Only in Draft status: update notes, adjust line item quantities.
func (h *Handler) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(chi.URLParam(r, "orderId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}

	var body struct {
		Notes     *string `json:"notes"`
		LineItems []struct {
			ID       string `json:"id"`
			Quantity int32  `json:"quantity"`
		} `json:"line_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if body.Notes != nil {
		_, err := h.Queries.UpdateOrderNotes(r.Context(), dbgen.UpdateOrderNotesParams{
			Notes:  body.Notes,
			ID:     orderID,
			TeamID: teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "order not found or not in draft status", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	for _, li := range body.LineItems {
		var liID pgtype.UUID
		if err := liID.Scan(li.ID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid line item id", "BAD_REQUEST")
			return
		}
		if err := h.Queries.UpdateOrderLineItemQuantity(r.Context(), dbgen.UpdateOrderLineItemQuantityParams{
			ID:              liID,
			QuantityOrdered: li.Quantity,
			TeamID:          teamID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// CancelOrder handles DELETE /api/v1/teams/{teamId}/orders/{orderId}
// Transitions order to cancelled (any status except Received).
func (h *Handler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(chi.URLParam(r, "orderId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}

	if err := h.Queries.CancelOrder(r.Context(), dbgen.CancelOrderParams{
		ID:     orderID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ExportOrder handles GET /api/v1/teams/{teamId}/orders/{orderId}/export
// Generates and downloads CSV. Transitions to exported.
func (h *Handler) ExportOrder(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(chi.URLParam(r, "orderId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}

	order, err := h.Queries.GetOrder(r.Context(), dbgen.GetOrderParams{
		ID:     orderID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	lineItems, err := h.Queries.GetOrderLineItems(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Transition to exported.
	if err := h.Queries.TransitionOrderToExported(r.Context(), dbgen.TransitionOrderToExportedParams{
		ID:     orderID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})

	filename := fmt.Sprintf("order-%s-%s.csv", order.DistributorName, order.CreatedAt.Time.UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	cw := csv.NewWriter(w)
	cw.Write([]string{"SKU", "Part MPN", "Packaging Type", "Quantity", "Unit Price", "Fixed Order Cost"}) //nolint:errcheck
	for _, li := range lineItems {
		mpn := ""
		if li.PartMpn != nil {
			mpn = *li.PartMpn
		}
		unitPrice := ""
		if s := numericString(li.UnitPriceSnapshot); s != nil {
			unitPrice = *s
		}
		fixedCost := ""
		if s := numericString(li.FixedOrderCostSnapshot); s != nil {
			fixedCost = *s
		}
		cw.Write([]string{ //nolint:errcheck
			li.Sku,
			mpn,
			li.PackagingType,
			fmt.Sprintf("%d", li.QuantityOrdered),
			unitPrice,
			fixedCost,
		})
	}
	cw.Flush()
}

// SubmitOrder handles POST /api/v1/teams/{teamId}/orders/{orderId}/submit
// Submits via distributor API. Transitions to submitted or placed.
func (h *Handler) SubmitOrder(w http.ResponseWriter, r *http.Request) {
	// Distributor API integration is not yet implemented.
	// Return 501 with a descriptive message.
	writeError(w, http.StatusNotImplemented,
		"distributor API submission is not yet implemented; use export+reference flow instead",
		"NOT_IMPLEMENTED")
}

// LinkOrderReference handles POST /api/v1/teams/{teamId}/orders/{orderId}/reference
// Body: {external_reference}. Transitions to placed.
func (h *Handler) LinkOrderReference(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(chi.URLParam(r, "orderId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}

	var body struct {
		ExternalReference *string `json:"external_reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.LinkOrderExternalReference(r.Context(), dbgen.LinkOrderExternalReferenceParams{
		ExternalReference: body.ExternalReference,
		ID:                orderID,
		TeamID:            teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found or already placed/received/cancelled", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})

	resp := map[string]any{
		"id":                   row.ID.String(),
		"status":               string(row.Status),
		"external_reference":   row.ExternalReference,
	}
	if row.ExternalRefLinkedAt.Valid {
		resp["external_ref_linked_at"] = row.ExternalRefLinkedAt.Time.UTC().Format(time.RFC3339)
	} else {
		resp["external_ref_linked_at"] = nil
	}

	writeJSON(w, http.StatusOK, resp)
}

// ReceiveOrderItem handles POST /api/v1/teams/{teamId}/orders/{orderId}/items/{itemId}/receive
// Body: {quantity_received}. Creates stock adjustment. Auto-transitions order status.
func (h *Handler) ReceiveOrderItem(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(chi.URLParam(r, "orderId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}
	var lineItemID pgtype.UUID
	if err := lineItemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
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
		QuantityReceived int32  `json:"quantity_received"`
		Reason           string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.QuantityReceived <= 0 {
		writeError(w, http.StatusBadRequest, "quantity_received must be positive", "BAD_REQUEST")
		return
	}

	// Fetch the order line item to get part_id and verify team ownership.
	li, err := h.Queries.GetOrderLineItem(r.Context(), dbgen.GetOrderLineItemParams{
		ID:     lineItemID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order line item not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Get or create the inventory entry for this part.
	invEntry, err := h.Queries.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID,
		PartID: li.PartID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnprocessableEntity,
				"no inventory entry exists for this part; create one first",
				"NO_INVENTORY_ENTRY")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	// Update quantity_received on the line item.
	updatedLI, err := qtx.ReceiveOrderLineItem(r.Context(), dbgen.ReceiveOrderLineItemParams{
		ID:               lineItemID,
		QuantityReceived: body.QuantityReceived,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Create the stock adjustment.
	var reason *string
	if body.Reason != "" {
		reason = &body.Reason
	}
	if _, err := qtx.CreateOrderStockAdjustment(r.Context(), dbgen.CreateOrderStockAdjustmentParams{
		InventoryEntryID: invEntry.ID,
		Delta:            body.QuantityReceived,
		Reason:           reason,
		CreatedBy:        userID,
		OrderLineItemID:  lineItemID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Apply the stock delta.
	newQty, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
		ID:             invEntry.ID,
		QuantityOnHand: body.QuantityReceived,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Check if order is now fully or partially received and transition status.
	receiptStatus, err := qtx.GetOrderReceiptStatus(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var newOrderStatus dbgen.OrderStatus
	if receiptStatus.Total > 0 && receiptStatus.FullyReceived == receiptStatus.Total {
		newOrderStatus = dbgen.OrderStatusReceived
	} else {
		newOrderStatus = dbgen.OrderStatusPartiallyReceived
	}

	if err := qtx.TransitionOrderStatus(r.Context(), dbgen.TransitionOrderStatusParams{
		ID:     orderID,
		Status: newOrderStatus,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})
	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{"part_id": li.PartID.String(), "entry_id": invEntry.ID.String()})

	writeJSON(w, http.StatusOK, map[string]any{
		"line_item_id":        updatedLI.ID.String(),
		"quantity_ordered":    updatedLI.QuantityOrdered,
		"quantity_received":   updatedLI.QuantityReceived,
		"order_status":        string(newOrderStatus),
		"new_quantity_on_hand": newQty,
	})
}

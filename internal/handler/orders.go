package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func orderResponse(o dbgen.GetOrderRow) map[string]any {
	resp := map[string]any{
		"id":                      o.ID.String(),
		"distributor_id":          o.DistributorID.String(),
		"distributor_name":        o.DistributorName,
		"distributor_api_type":    o.DistributorApiType,
		"status":                  string(o.Status),
		"external_reference":      o.ExternalReference,
		"notes":                   o.Notes,
		"purchase_order_number":   o.PurchaseOrderNumber,
		"created_at":              o.CreatedAt.Time.UTC().Format(time.RFC3339),
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
	// integrations: return as raw JSON object (nil/empty stays as null/empty)
	if len(o.Integrations) > 0 {
		var raw json.RawMessage = o.Integrations
		resp["integrations"] = raw
	} else {
		resp["integrations"] = json.RawMessage("{}")
	}
	return resp
}

func orderLineItemResponse(li dbgen.GetOrderLineItemsRow) map[string]any {
	return map[string]any{
		"id":                        li.ID.String(),
		"listing_id":                li.ListingID.String(),
		"part_mpn":                  li.PartMpn,
		"sku":                       li.Sku,
		"packaging_type":            li.PackagingType,
		"quantity_ordered":          li.QuantityOrdered,
		"quantity_received":         li.QuantityReceived,
		"unit_price_snapshot":       numericString(li.UnitPriceSnapshot),
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
			Valid:       true,
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

	items := make([]orderItem, len(rows))
	for i, o := range rows {
		items[i] = orderItem{
			ID:                   o.ID.String(),
			DistributorID:        o.DistributorID.String(),
			DistributorName:      o.DistributorName,
			Status:               string(o.Status),
			ExternalReference:    o.ExternalReference,
			CreatedAt:            o.CreatedAt.Time.UTC().Format(time.RFC3339),
			CreatedByDisplayName: o.CreatedByDisplayName,
			LineItemCount:        o.ItemCount,
			TotalPrice:           numericString(o.TotalPrice),
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
	resp["line_item_count"] = len(items)
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
		Notes               *string `json:"notes"`
		PurchaseOrderNumber *string `json:"purchase_order_number"`
		LineItems           []struct {
			ID       string `json:"id"`
			Quantity int32  `json:"quantity"`
		} `json:"line_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if body.PurchaseOrderNumber != nil {
		_, err := h.Queries.UpdateOrderPurchaseOrderNumber(r.Context(), dbgen.UpdateOrderPurchaseOrderNumberParams{
			PurchaseOrderNumber: body.PurchaseOrderNumber,
			ID:                  orderID,
			TeamID:              teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
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
		"id":                 row.ID.String(),
		"status":             string(row.Status),
		"external_reference": row.ExternalReference,
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
// Also allocates received quantity to linked builds (US-B7, US-B8).
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
		QuantityReceived int32 `json:"quantity_received"`
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

	// Guard: only allow receiving on placed or partially_received orders.
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
	if order.Status != dbgen.OrderStatusPlaced && order.Status != dbgen.OrderStatusPartiallyReceived {
		writeError(w, http.StatusConflict, "order must be in placed or partially_received status to receive items", "ORDER_NOT_RECEIVABLE")
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
	defaultReason := fmt.Sprintf("Received from order %s", orderID.String())
	if _, err := qtx.CreateOrderStockAdjustment(r.Context(), dbgen.CreateOrderStockAdjustmentParams{
		InventoryEntryID: invEntry.ID,
		Delta:            body.QuantityReceived,
		Reason:           &defaultReason,
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

	// Allocate received qty to linked builds (US-B7, US-B8).
	linkedBuildIDs, err := qtx.GetLinkedBuildIDsForOrder(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type allocationBreakdownItem struct {
		BuildID   string `json:"build_id"`
		Allocated int32  `json:"allocated"`
	}
	var allocationBreakdown []allocationBreakdownItem
	var buildIDsToCheckReady []pgtype.UUID

	// Fetch all allocations for (order, part) once, then distribute in order.
	allocs, err := qtx.GetLinkedBuildAllocationsForOrderPart(r.Context(), dbgen.GetLinkedBuildAllocationsForOrderPartParams{
		OrderID: orderID,
		PartID:  li.PartID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	// Index allocs by build_id for O(1) lookup.
	allocByBuild := make(map[string]dbgen.GetLinkedBuildAllocationsForOrderPartRow, len(allocs))
	for _, a := range allocs {
		allocByBuild[a.BuildID.String()] = a
	}

	remaining := body.QuantityReceived
	for _, bid := range linkedBuildIDs {
		if remaining <= 0 {
			break
		}
		a, ok := allocByBuild[bid.String()]
		if !ok {
			continue
		}
		stillNeeded := a.QuantityNeeded - a.QuantityReserved - a.QuantityReceived
		if stillNeeded <= 0 {
			continue
		}
		allocate := min(remaining, stillNeeded)
		if allocate > 0 {
			if err := qtx.IncrementBuildPartAllocationReceived(r.Context(), dbgen.IncrementBuildPartAllocationReceivedParams{
				BuildID:          bid,
				PartID:           li.PartID,
				QuantityReceived: allocate,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			remaining -= allocate
			allocationBreakdown = append(allocationBreakdown, allocationBreakdownItem{
				BuildID:   bid.String(),
				Allocated: allocate,
			})
			buildIDsToCheckReady = append(buildIDsToCheckReady, bid)
		}
	}

	// Check readiness for builds that received allocation.
	var readyBuildIDs []pgtype.UUID
	for _, bid := range buildIDsToCheckReady {
		isReady, err := qtx.CheckBuildReadiness(r.Context(), bid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if isReady {
			readyBuildIDs = append(readyBuildIDs, bid)
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})
	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{"part_id": li.PartID.String(), "entry_id": invEntry.ID.String()})

	// Transition ready builds and publish events.
	for _, bid := range readyBuildIDs {
		_ = h.Queries.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
			ID:     bid,
			Status: "ready",
			TeamID: teamID,
		})
		h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": bid.String()})
	}

	if allocationBreakdown == nil {
		allocationBreakdown = []allocationBreakdownItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"line_item_id":         updatedLI.ID.String(),
		"quantity_ordered":     updatedLI.QuantityOrdered,
		"quantity_received":    updatedLI.QuantityReceived,
		"order_status":         string(newOrderStatus),
		"new_quantity_on_hand": newQty,
		"allocation_breakdown": allocationBreakdown,
	})
}

// ---- Distributor API ordering helpers ----

// getOrderingClient returns the OrderingClient for the order's distributor, or an error.
func (h *Handler) getOrderingClient(order dbgen.GetOrderRow) (distributor.OrderingClient, error) {
	if order.DistributorApiType == nil || *order.DistributorApiType == "" {
		return nil, fmt.Errorf("distributor has no API integration")
	}
	client := h.Distributors.Lookup(*order.DistributorApiType)
	if client == nil {
		return nil, fmt.Errorf("unknown distributor: %s", *order.DistributorApiType)
	}
	oc, ok := client.(distributor.OrderingClient)
	if !ok {
		return nil, fmt.Errorf("distributor does not support ordering APIs")
	}
	return oc, nil
}

// mergeDigiKeyIntegrations deep-merges patch into the digikey sub-object of integrations JSONB.
func mergeDigiKeyIntegrations(current []byte, patch map[string]any) ([]byte, error) {
	var top map[string]any
	if len(current) > 0 {
		if err := json.Unmarshal(current, &top); err != nil {
			top = make(map[string]any)
		}
	} else {
		top = make(map[string]any)
	}

	dk, _ := top["digikey"].(map[string]any)
	if dk == nil {
		dk = make(map[string]any)
	}
	for k, v := range patch {
		dk[k] = v
	}
	top["digikey"] = dk
	return json.Marshal(top)
}

// PushToDistributor handles POST /api/v1/teams/{teamId}/orders/{orderId}/push-to-distributor
// Role: purchaser+
// Creates a parts list on the distributor's platform (e.g. DigiKey MyList).
func (h *Handler) PushToDistributor(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()

	order, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if order.Status == dbgen.OrderStatusCancelled || order.Status == dbgen.OrderStatusReceived {
		writeError(w, http.StatusConflict, "cannot push cancelled or received order", "ORDER_NOT_PUSHABLE")
		return
	}

	oc, err := h.getOrderingClient(order)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "NO_ORDERING_SUPPORT")
		return
	}

	creds, err := h.getThreeLeggedCreds(r, order)
	if errors.Is(err, ErrOAuthRequired) {
		writeError(w, http.StatusUnauthorized, "OAuth authorization required", "OAUTH_REQUIRED")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get credentials", "INTERNAL_ERROR")
		return
	}

	lineItems, err := h.Queries.GetOrderLineItems(ctx, orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items := make([]distributor.OrderItem, len(lineItems))
	for i, li := range lineItems {
		items[i] = distributor.OrderItem{
			SKU:               li.Sku,
			Quantity:          li.QuantityOrdered,
			CustomerReference: li.ID.String(),
		}
	}

	listName := "partrack-" + orderID.String()[:8]
	result, listID, apiErr := oc.CreateList(ctx, listName, items, creds)

	// Persist listID regardless of partial failure.
	patch := map[string]any{"list_id": listID}
	if result != nil {
		patch["list_url"] = result.ListURL
	}
	updated, mergeErr := mergeDigiKeyIntegrations(order.Integrations, patch)
	if mergeErr == nil {
		_, _ = h.Queries.UpdateOrderIntegrations(ctx, dbgen.UpdateOrderIntegrationsParams{
			ID:           orderID,
			TeamID:       teamID,
			Integrations: updated,
		})
	}

	if apiErr != nil {
		writeError(w, http.StatusBadGateway,
			fmt.Sprintf("DigiKey list created but parts could not be added: %v", apiErr),
			"LIST_PARTS_FAILED")
		return
	}

	_, _ = h.Queries.TransitionOrderToExportedViaAPI(ctx, dbgen.TransitionOrderToExportedViaAPIParams{
		ID:     orderID,
		TeamID: teamID,
	})
	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})

	writeJSON(w, http.StatusOK, map[string]any{
		"list_id":  listID,
		"list_url": result.ListURL,
	})
}

// RequestQuote handles POST /api/v1/teams/{teamId}/orders/{orderId}/request-quote
// Role: purchaser+
// Creates a quote on the distributor's platform with confirmed pricing.
func (h *Handler) RequestQuote(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()

	order, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if order.Status == dbgen.OrderStatusCancelled || order.Status == dbgen.OrderStatusReceived {
		writeError(w, http.StatusConflict, "cannot quote cancelled or received order", "ORDER_NOT_QUOTABLE")
		return
	}

	oc, err := h.getOrderingClient(order)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "NO_ORDERING_SUPPORT")
		return
	}

	creds, err := h.getThreeLeggedCreds(r, order)
	if errors.Is(err, ErrOAuthRequired) {
		writeError(w, http.StatusUnauthorized, "OAuth authorization required", "OAUTH_REQUIRED")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get credentials", "INTERNAL_ERROR")
		return
	}

	lineItems, err := h.Queries.GetOrderLineItems(ctx, orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items := make([]distributor.OrderItem, len(lineItems))
	for i, li := range lineItems {
		items[i] = distributor.OrderItem{
			SKU:               li.Sku,
			Quantity:          li.QuantityOrdered,
			CustomerReference: li.ID.String(),
		}
	}

	quoteName := "partrack-" + orderID.String()[:8]
	quoteResult, err := oc.RequestQuote(ctx, quoteName, items, creds)
	if err != nil {
		// A 400 from DigiKey typically means missing config (e.g. account_id not set).
		if strings.Contains(err.Error(), "400") {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("DigiKey rejected the quote — check that account_id is configured in your DigiKey distributor settings: %v", err),
				"QUOTE_CONFIG_ERROR")
			return
		}
		writeError(w, http.StatusBadGateway, fmt.Sprintf("quote request failed: %v", err), "QUOTE_FAILED")
		return
	}

	// Serialize quote items for storage.
	quoteItemsJSON, _ := json.Marshal(quoteResult.Items)
	var quoteItemsRaw json.RawMessage = quoteItemsJSON

	patch := map[string]any{
		"quote_id":    quoteResult.QuoteID,
		"quote_name":  quoteResult.QuoteName,
		"quote_items": quoteItemsRaw,
	}
	updated, mergeErr := mergeDigiKeyIntegrations(order.Integrations, patch)
	if mergeErr == nil {
		_, _ = h.Queries.UpdateOrderIntegrations(ctx, dbgen.UpdateOrderIntegrationsParams{
			ID:           orderID,
			TeamID:       teamID,
			Integrations: updated,
		})
	}

	_, _ = h.Queries.TransitionOrderToExportedViaAPI(ctx, dbgen.TransitionOrderToExportedViaAPIParams{
		ID:     orderID,
		TeamID: teamID,
	})
	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})

	writeJSON(w, http.StatusOK, map[string]any{
		"quote_id":   quoteResult.QuoteID,
		"quote_name": quoteResult.QuoteName,
		"items":      quoteResult.Items,
	})
}

// GetDistributorStatus handles GET /api/v1/teams/{teamId}/orders/{orderId}/distributor-status
// Role: viewer+
// Fetches live order status from the distributor using external_reference.
func (h *Handler) GetDistributorStatus(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()

	order, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if order.ExternalReference == nil || *order.ExternalReference == "" {
		writeError(w, http.StatusUnprocessableEntity, "order has no external_reference (DigiKey sales order number)", "NO_EXTERNAL_REFERENCE")
		return
	}

	oc, err := h.getOrderingClient(order)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "NO_ORDERING_SUPPORT")
		return
	}

	// Status check uses 2-legged credentials — no 3-legged token needed.
	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        order.TeamID,
		DistributorID: order.DistributorID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "distributor config not found", "CONFIG_NOT_FOUND")
		return
	}

	statusResult, err := oc.GetOrderStatus(ctx, *order.ExternalReference, cfg.ApiCredentials)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("status fetch failed: %v", err), "STATUS_FETCH_FAILED")
		return
	}

	// Persist last status into integrations.
	patch := map[string]any{
		"last_status":            statusResult.Status,
		"last_status_detail":     statusResult.StatusDetail,
		"last_status_checked_at": statusResult.LastCheckedAt.UTC().Format(time.RFC3339),
	}
	updated, mergeErr := mergeDigiKeyIntegrations(order.Integrations, patch)
	if mergeErr == nil {
		_, _ = h.Queries.UpdateOrderIntegrations(ctx, dbgen.UpdateOrderIntegrationsParams{
			ID:           orderID,
			TeamID:       teamID,
			Integrations: updated,
		})
	}
	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         statusResult.Status,
		"status_detail":  statusResult.StatusDetail,
		"line_items":     statusResult.LineItems,
		"last_checked_at": statusResult.LastCheckedAt.UTC().Format(time.RFC3339),
	})
}

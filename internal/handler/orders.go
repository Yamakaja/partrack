package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
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
		"part_id":                   li.PartID.String(),
		"part_mpn":                  li.PartMpn,
		"part_manufacturer":         li.PartManufacturer,
		"part_description":          li.PartDescription,
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

func linkedBuildResponse(b dbgen.GetLinkedBuildsForOrderRow) map[string]any {
	resp := map[string]any{
		"id":                   b.ID.String(),
		"project_id":           b.ProjectID.String(),
		"project_name":         b.ProjectName,
		"bom_revision_id":      b.BomRevisionID.String(),
		"bom_revision_label":   b.BomRevisionLabel,
		"quantity":             b.Quantity,
		"status":               b.Status,
		"created_at":           b.CreatedAt.Time.UTC().Format(time.RFC3339),
		"total_line_items":     b.TotalLineItems,
		"fulfilled_line_items": b.FulfilledLineItems,
	}
	if b.CreatedByDisplayName != nil {
		resp["created_by_display_name"] = *b.CreatedByDisplayName
	} else {
		resp["created_by_display_name"] = nil
	}
	return resp
}

// GetOrderLinkedBuilds handles GET /api/v1/teams/{teamId}/orders/{orderId}/linked-builds
func (h *Handler) GetOrderLinkedBuilds(w http.ResponseWriter, r *http.Request) {
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

	// Verify order belongs to team.
	if _, err := h.Queries.GetOrder(r.Context(), dbgen.GetOrderParams{ID: orderID, TeamID: teamID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	builds, err := h.Queries.GetLinkedBuildsForOrder(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	result := make([]map[string]any, len(builds))
	for i, b := range builds {
		result[i] = linkedBuildResponse(b)
	}
	writeJSON(w, http.StatusOK, result)
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
				writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
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
// Transitions order to cancelled (any status except Received). Linked builds in
// ordered/ready state are reverted to pending and BOM shortfalls are recomputed.
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

	status, err := h.Queries.GetOrderStatusForTeam(r.Context(), dbgen.GetOrderStatusForTeamParams{
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
	if status == "received" {
		writeError(w, http.StatusConflict, "cannot cancel a received order", "INVALID_STATUS")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	if err := qtx.CancelOrder(r.Context(), dbgen.CancelOrderParams{
		ID:     orderID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	linkedBuildIDs, err := qtx.GetLinkedBuildIDsForOrder(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var revertedBuildIDs []pgtype.UUID
	for _, buildID := range linkedBuildIDs {
		build, err := qtx.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
			ID:     buildID,
			TeamID: teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if build.Status != "ordered" && build.Status != "ready" {
			continue
		}
		if err := qtx.DeleteBuildPartAllocations(r.Context(), buildID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if err := qtx.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
			ID:     buildID,
			Status: "pending",
			TeamID: teamID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		revertedBuildIDs = append(revertedBuildIDs, buildID)
	}

	if err := qtx.DeleteBuildOrdersByOrder(r.Context(), orderID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if len(revertedBuildIDs) > 0 {
		cart, err := qtx.GetOrCreateCart(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if _, err := h.recomputeBomShortfalls(r.Context(), qtx, cart.ID, teamID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "order.cancelled",
		Feed:    activity.FeedTeam,
		Subject: &activity.Ref{Type: "order", ID: orderID.String()},
		Summary: "Cancelled an order",
	})
	if len(revertedBuildIDs) > 0 {
		for _, buildID := range revertedBuildIDs {
			h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
		}
		h.CartResolver.Invalidate(teamID.String())
		h.publishCartChanged(r, teamID)
	}
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
	linkSummary := "Linked a distributor order reference"
	if body.ExternalReference != nil && *body.ExternalReference != "" {
		linkSummary = "Linked distributor order " + *body.ExternalReference
	}
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "order.external_linked",
		Feed:    activity.FeedTeam,
		Subject: &activity.Ref{Type: "order", ID: orderID.String(), Label: derefStr(row.ExternalReference)},
		Summary: linkSummary,
	})

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

	defaultReason := fmt.Sprintf("Received from order %s", orderID.String())
	adj, err := qtx.CreateOrderStockAdjustment(r.Context(), dbgen.CreateOrderStockAdjustmentParams{
		InventoryEntryID: invEntry.ID,
		Delta:            body.QuantityReceived,
		Reason:           &defaultReason,
		CreatedBy:        userID,
		OrderLineItemID:  lineItemID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	newQty := adj.NewQuantityOnHand

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
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "order.item_received",
		Feed:    activity.FeedTeam,
		Subject: &activity.Ref{Type: "order", ID: orderID.String()},
		Refs:    map[string]activity.Ref{"part": {Type: "part", ID: li.PartID.String()}},
		Values:  map[string]any{"qty": body.QuantityReceived},
		Summary: fmt.Sprintf("Received %d unit(s) on an order", body.QuantityReceived),
	})

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

// mergeDistributorIntegrations deep-merges patch into the distributor's sub-object of integrations JSONB.
// apiType is used as the sub-object key (e.g. "digikey").
func mergeDistributorIntegrations(apiType string, current []byte, patch map[string]any) ([]byte, error) {
	var top map[string]any
	if len(current) > 0 {
		if err := json.Unmarshal(current, &top); err != nil {
			top = make(map[string]any)
		}
	} else {
		top = make(map[string]any)
	}

	sub, _ := top[apiType].(map[string]any)
	if sub == nil {
		sub = make(map[string]any)
	}
	for k, v := range patch {
		sub[k] = v
	}
	top[apiType] = sub
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

	creds, err := h.getEnrichedCreds(r, order)
	if errors.Is(err, distributor.ErrOAuthRequired) {
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
	updated, mergeErr := mergeDistributorIntegrations(*order.DistributorApiType, order.Integrations, patch)
	if mergeErr == nil {
		_, _ = h.Queries.UpdateOrderIntegrations(ctx, dbgen.UpdateOrderIntegrationsParams{
			ID:           orderID,
			TeamID:       teamID,
			Integrations: updated,
		})
	}

	if apiErr != nil {
		writeError(w, http.StatusBadGateway,
			fmt.Sprintf("list created but some parts could not be added: %v", apiErr),
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

	creds, err := h.getEnrichedCreds(r, order)
	if errors.Is(err, distributor.ErrOAuthRequired) {
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
		if strings.Contains(err.Error(), "400") {
			writeError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("distributor rejected the quote request: %v", err),
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
	updated, mergeErr := mergeDistributorIntegrations(*order.DistributorApiType, order.Integrations, patch)
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
		writeError(w, http.StatusUnprocessableEntity, "order has no external_reference", "NO_EXTERNAL_REFERENCE")
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

	// Serialize line items for JSONB storage (reuse JSON tags on the distributor types).
	lineItemsJSON, _ := json.Marshal(statusResult.LineItems)

	// Persist all status fields into integrations.
	patch := map[string]any{
		"last_status":                  statusResult.Status,
		"last_status_detail":           statusResult.StatusDetail,
		"last_status_checked_at":       statusResult.LastCheckedAt.UTC().Format(time.RFC3339),
		"last_status_order_number":     statusResult.OrderNumber,
		"last_status_currency":         statusResult.Currency,
		"last_status_total_price":      statusResult.TotalPrice,
		"last_status_ship_method":      statusResult.ShipMethod,
		"last_status_shipping_address": statusResult.ShippingAddress,
		"last_status_contact":          statusResult.Contact,
		"last_status_line_items":       json.RawMessage(lineItemsJSON),
	}
	updated, mergeErr := mergeDistributorIntegrations(*order.DistributorApiType, order.Integrations, patch)
	if mergeErr == nil {
		_, _ = h.Queries.UpdateOrderIntegrations(ctx, dbgen.UpdateOrderIntegrationsParams{
			ID:           orderID,
			TeamID:       teamID,
			Integrations: updated,
		})
	}
	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderID.String()})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           statusResult.Status,
		"status_detail":    statusResult.StatusDetail,
		"last_checked_at":  statusResult.LastCheckedAt.UTC().Format(time.RFC3339),
		"order_number":     statusResult.OrderNumber,
		"currency":         statusResult.Currency,
		"total_price":      statusResult.TotalPrice,
		"ship_method":      statusResult.ShipMethod,
		"shipping_address": statusResult.ShippingAddress,
		"contact":          statusResult.Contact,
		"line_items":       statusResult.LineItems,
	})
}

// RetryUnresolvedItems handles POST /orders/{orderId}/retry-unresolved.
// Enqueues a job to re-attempt DigiKey enrichment for unresolved import items.
// Requires: order linked to DigiKey (external_reference set) and not cancelled.
// Role: purchaser+
func (h *Handler) RetryUnresolvedItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	user := middleware.UserFromContext(ctx)

	order, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if order.ExternalReference == nil {
		writeError(w, http.StatusBadRequest, "order not linked to DigiKey", "NOT_LINKED")
		return
	}
	if order.Status == "cancelled" {
		writeError(w, http.StatusConflict, "order is cancelled", "ORDER_CANCELLED")
		return
	}

	jobID := h.Worker.Enqueue(worker.JobRetryUnresolved, teamID.String(), map[string]string{
		"order_id": orderID.String(),
		"user_id":  user.ID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"job_id": jobID})
}

// ImportOrder handles POST /orders/import.
// Body: { "sales_order_id": "...", "existing_order_id": "uuid" (optional), "force": bool (optional) }
//
// Case A (existing_order_id present): synchronous mismatch pre-flight, then enqueue JobOrderLink.
// Case B (no existing_order_id): enqueue JobOrderImport immediately.
func (h *Handler) ImportOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	user := middleware.UserFromContext(ctx)

	var body struct {
		SalesOrderID    string `json:"sales_order_id"`
		ExistingOrderID string `json:"existing_order_id"`
		Force           bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.SalesOrderID == "" {
		writeError(w, http.StatusUnprocessableEntity, "sales_order_id is required", "MISSING_SALES_ORDER_ID")
		return
	}

	if body.ExistingOrderID != "" {
		// Case A: link existing partrack order.
		h.importOrderLink(w, r, teamID, user, body.SalesOrderID, body.ExistingOrderID, body.Force)
	} else {
		// Case B: create new order from DigiKey data.
		h.importOrderNew(w, r, teamID, user, body.SalesOrderID)
	}
}

func (h *Handler) importOrderNew(w http.ResponseWriter, r *http.Request, teamID pgtype.UUID, user *middleware.User, salesOrderID string) {
	ctx := r.Context()
	apiType := "digikey"

	dist, err := h.Queries.GetDistributorByApiType(ctx, &apiType)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "DigiKey distributor not configured", "DISTRIBUTOR_NOT_FOUND")
		return
	}

	if _, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: dist.ID,
	}); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "DigiKey not configured for this team", "CONFIG_NOT_FOUND")
		return
	}

	// Duplicate check.
	if _, err := h.Queries.GetOrderByExternalReference(ctx, dbgen.GetOrderByExternalReferenceParams{
		TeamID:            teamID,
		ExternalReference: &salesOrderID,
	}); err == nil {
		writeError(w, http.StatusConflict, "order already imported", "ALREADY_IMPORTED")
		return
	}

	jobID := h.Worker.Enqueue(worker.JobOrderImport, teamID.String(), map[string]string{
		"sales_order_id": salesOrderID,
		"distributor_id": dist.ID.String(),
		"user_id":        user.ID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"job_id": jobID})
}

// linkTokenPayload is the signed data carried by the client between the
// conflict-detection preflight and the commit endpoint.
type linkTokenPayload struct {
	OrderID      string          `json:"order_id"`
	TeamID       string          `json:"team_id"`
	SalesOrderID string          `json:"sales_order_id"`
	ExpiresAt    time.Time       `json:"expires_at"`
	LineItems    []linkTokenItem `json:"line_items"`
}

type linkTokenItem struct {
	LineItemID      string `json:"line_item_id"`
	SKU             string `json:"sku"`
	PlannedQty      int32  `json:"planned_qty"`
	ActualQty       int32  `json:"actual_qty"`
	ActualPrice     string `json:"actual_price"`
	ActualShipped   int32  `json:"actual_shipped"`
	ActualBackorder int32  `json:"actual_backorder"`
}

// partShortfallInfo describes a single part where a build loses order coverage.
type partShortfallInfo struct {
	PartID                  string `json:"part_id"`
	MPN                     string `json:"mpn"`
	Description             string `json:"description"`
	QuantityFromOrderBefore int32  `json:"quantity_from_order_before"`
	ActualQty               int32  `json:"actual_qty"`
}

// affectedBuildInfo describes a build affected by a quantity reduction.
type affectedBuildInfo struct {
	BuildID       string              `json:"build_id"`
	ProjectName   string              `json:"project_name"`
	BuildQuantity int32               `json:"build_quantity"`
	Status        string              `json:"status"`
	SuggestedKeep bool                `json:"suggested_keep"`
	PartsShort    []partShortfallInfo `json:"parts_short"`
}

func (h *Handler) importOrderLink(w http.ResponseWriter, r *http.Request, teamID pgtype.UUID, user *middleware.User, salesOrderID, existingOrderIDStr string, force bool) {
	ctx := r.Context()

	var orderID pgtype.UUID
	if err := orderID.Scan(existingOrderIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid existing_order_id", "BAD_REQUEST")
		return
	}

	order, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
		return
	}

	// Validate distributor is DigiKey and order is in a linkable status.
	if order.DistributorApiType == nil || *order.DistributorApiType != "digikey" {
		writeError(w, http.StatusUnprocessableEntity, "order distributor is not DigiKey", "WRONG_DISTRIBUTOR")
		return
	}
	validStatuses := map[string]bool{"draft": true, "exported": true, "submitted": true, "placed": true}
	if !validStatuses[string(order.Status)] {
		writeError(w, http.StatusUnprocessableEntity, "order is not in a linkable status", "WRONG_STATUS")
		return
	}
	if order.ExternalReference != nil {
		writeError(w, http.StatusUnprocessableEntity, "order is already linked to an external reference", "ALREADY_LINKED")
		return
	}

	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: order.DistributorID,
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "distributor not configured", "CONFIG_NOT_FOUND")
		return
	}

	oc, err := h.getOrderingClient(order)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "NO_ORDERING_SUPPORT")
		return
	}

	// Synchronous pre-flight: fetch DigiKey order to validate and check for mismatches.
	dkOrder, err := oc.GetOrderStatus(ctx, salesOrderID, cfg.ApiCredentials)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch DigiKey order: "+err.Error(), "FETCH_FAILED")
		return
	}

	// Build DigiKey qty/price map by SKU.
	type dkItem struct {
		qty       int32
		price     string
		shipped   int32
		backorder int32
	}
	dkBySKU := make(map[string]dkItem, len(dkOrder.LineItems))
	for _, li := range dkOrder.LineItems {
		dkBySKU[li.SKU] = dkItem{
			qty:       li.QuantityOrdered,
			price:     fmt.Sprintf("%.4f", li.UnitPrice),
			shipped:   li.QuantityShipped,
			backorder: li.QuantityBackOrder,
		}
	}

	// Fetch partrack line items with part IDs.
	ptItems, err := h.Queries.GetOrderLineItemsWithPartID(ctx, orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get line items", "INTERNAL")
		return
	}

	if !force {
		// Check for order_mismatch: >10% of SKUs or qty completely absent from DigiKey.
		var missingSKUs []string
		var totalQty, missingQty int32
		for _, li := range ptItems {
			totalQty += li.QuantityOrdered
			if _, found := dkBySKU[li.Sku]; !found {
				missingSKUs = append(missingSKUs, li.Sku)
				missingQty += li.QuantityOrdered
			}
		}
		if len(ptItems) > 0 && len(missingSKUs) > 0 {
			skuPct := float64(len(missingSKUs)) / float64(len(ptItems)) * 100
			qtyPct := float64(0)
			if totalQty > 0 {
				qtyPct = float64(missingQty) / float64(totalQty) * 100
			}
			if skuPct > 10 || qtyPct > 10 {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"code":            "order_mismatch",
					"missing_skus":    missingSKUs,
					"missing_qty_pct": qtyPct,
					"missing_sku_pct": skuPct,
				})
				return
			}
		}
	}

	// Collect token items (all items where DigiKey qty differs from planned).
	var tokenItems []linkTokenItem
	for _, li := range ptItems {
		dk := dkBySKU[li.Sku] // zero value if absent → actualQty = 0
		if dk.qty != li.QuantityOrdered {
			tokenItems = append(tokenItems, linkTokenItem{
				LineItemID:      li.ID.String(),
				SKU:             li.Sku,
				PlannedQty:      li.QuantityOrdered,
				ActualQty:       dk.qty,
				ActualPrice:     dk.price,
				ActualShipped:   dk.shipped,
				ActualBackorder: dk.backorder,
			})
		}
	}

	// Check for allocation conflicts: any part where qty decreased and build claims exceed new qty.
	// Build a map: partID → {actualQty, []allocation}
	type partConflict struct {
		actualQty   int32
		allocations []dbgen.GetLinkedBuildAllocationsForOrderPartRow
	}
	partConflicts := make(map[string]partConflict)

	for _, li := range ptItems {
		dk := dkBySKU[li.Sku]
		if dk.qty >= li.QuantityOrdered {
			continue // no reduction for this part
		}
		allocs, err := h.Queries.GetLinkedBuildAllocationsForOrderPart(ctx, dbgen.GetLinkedBuildAllocationsForOrderPartParams{
			OrderID: orderID,
			PartID:  li.PartID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get build allocations", "INTERNAL")
			return
		}
		var totalClaims int32
		for _, a := range allocs {
			claim := a.QuantityNeeded - a.QuantityReserved - a.QuantityReceived
			if claim < 0 {
				claim = 0
			}
			totalClaims += claim
		}
		if totalClaims > dk.qty {
			partConflicts[li.PartID.String()] = partConflict{actualQty: dk.qty, allocations: allocs}
		}
	}

	if len(partConflicts) > 0 {
		// Fetch build details for all linked builds.
		linkedBuilds, err := h.Queries.GetLinkedBuildsForOrder(ctx, orderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get linked builds", "INTERNAL")
			return
		}

		// Index allocations by buildID+partID.
		type buildPartKey struct{ buildID, partID string }
		allocByBuildPart := make(map[buildPartKey]int32)
		for _, pc := range partConflicts {
			for _, a := range pc.allocations {
				claim := a.QuantityNeeded - a.QuantityReserved - a.QuantityReceived
				if claim < 0 {
					claim = 0
				}
				allocByBuildPart[buildPartKey{a.BuildID.String(), a.PartID.String()}] = claim
			}
		}

		// Build affected builds list — only builds with at least one conflicting part claim.
		// Also fetch part details for mpn/description.
		type buildEntry struct {
			build      dbgen.GetLinkedBuildsForOrderRow
			partsShort []partShortfallInfo
		}
		var affected []buildEntry
		for _, b := range linkedBuilds {
			if b.Status != "ordered" && b.Status != "ready" {
				continue
			}
			var parts []partShortfallInfo
			for partIDStr, pc := range partConflicts {
				claim := allocByBuildPart[buildPartKey{b.ID.String(), partIDStr}]
				if claim == 0 {
					continue
				}
				// Fetch part info for mpn/description.
				var partID pgtype.UUID
				_ = partID.Scan(partIDStr)
				part, err := h.Queries.GetPart(ctx, partID)
				mpn := ""
				desc := ""
				if err == nil {
					if part.Mpn != nil {
						mpn = *part.Mpn
					}
					if part.Description != nil {
						desc = *part.Description
					}
				}
				parts = append(parts, partShortfallInfo{
					PartID:                  partIDStr,
					MPN:                     mpn,
					Description:             desc,
					QuantityFromOrderBefore: claim,
					ActualQty:               pc.actualQty,
				})
			}
			if len(parts) > 0 {
				affected = append(affected, buildEntry{build: b, partsShort: parts})
			}
		}

		// Greedy suggested_keep: sort by created_at ascending, assign claims until exhausted.
		sort.Slice(affected, func(i, j int) bool {
			return affected[i].build.CreatedAt.Time.Before(affected[j].build.CreatedAt.Time)
		})
		remainingByPart := make(map[string]int32)
		for partIDStr, pc := range partConflicts {
			remainingByPart[partIDStr] = pc.actualQty
		}
		suggestedKeep := make(map[string]bool, len(affected))
		for _, ae := range affected {
			fits := true
			for _, ps := range ae.partsShort {
				if ps.QuantityFromOrderBefore > remainingByPart[ps.PartID] {
					fits = false
					break
				}
			}
			if fits {
				suggestedKeep[ae.build.ID.String()] = true
				for _, ps := range ae.partsShort {
					remainingByPart[ps.PartID] -= ps.QuantityFromOrderBefore
				}
			}
		}

		// Build response.
		affectedBuilds := make([]affectedBuildInfo, 0, len(affected))
		for _, ae := range affected {
			affectedBuilds = append(affectedBuilds, affectedBuildInfo{
				BuildID:       ae.build.ID.String(),
				ProjectName:   ae.build.ProjectName,
				BuildQuantity: ae.build.Quantity,
				Status:        ae.build.Status,
				SuggestedKeep: suggestedKeep[ae.build.ID.String()],
				PartsShort:    ae.partsShort,
			})
		}

		// Sign token with all differing line items.
		token, err := signLinkToken(linkTokenPayload{
			OrderID:      existingOrderIDStr,
			TeamID:       teamID.String(),
			SalesOrderID: salesOrderID,
			ExpiresAt:    time.Now().Add(10 * time.Minute),
			LineItems:    tokenItems,
		}, h.Config.Auth.SessionSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to sign token", "INTERNAL")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"code":  "allocation_conflicts",
			"token": token,
			"conflicts": map[string]any{
				"affected_builds": affectedBuilds,
			},
		})
		return
	}

	jobID := h.Worker.Enqueue(worker.JobOrderLink, teamID.String(), map[string]any{
		"sales_order_id": salesOrderID,
		"order_id":       existingOrderIDStr,
		"distributor_id": order.DistributorID.String(),
		"user_id":        user.ID,
		"skip_reconcile": false,
	})

	writeJSON(w, http.StatusOK, map[string]string{"job_id": jobID})
}

// LinkCommit handles POST /orders/{orderId}/link-commit.
// The client submits a signed token (from the allocation_conflicts preflight) plus
// build priority decisions. The handler atomically updates line item quantities,
// reverts deselected builds to pending, recomputes shortfalls, and links the order.
func (h *Handler) LinkCommit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	orderIDStr := chi.URLParam(r, "orderId")
	var orderID pgtype.UUID
	if err := orderID.Scan(orderIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id", "BAD_REQUEST")
		return
	}
	user := middleware.UserFromContext(ctx)

	// Fetch the order to get the distributor ID (needed for job enqueue after commit).
	order, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		writeError(w, http.StatusNotFound, "order not found", "NOT_FOUND")
		return
	}

	var body struct {
		Token           string `json:"token"`
		BuildPriorities []struct {
			BuildID string `json:"build_id"`
			Keep    bool   `json:"keep"`
		} `json:"build_priorities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var payload linkTokenPayload
	if err := verifyLinkToken(body.Token, h.Config.Auth.SessionSecret, &payload); err != nil {
		if errors.Is(err, errTokenExpired) {
			writeError(w, http.StatusUnprocessableEntity, "link token has expired", "TOKEN_EXPIRED")
		} else {
			writeError(w, http.StatusUnprocessableEntity, "invalid link token", "INVALID_TOKEN")
		}
		return
	}

	if payload.OrderID != orderIDStr || payload.TeamID != teamID.String() {
		writeError(w, http.StatusForbidden, "token does not match this order", "TOKEN_MISMATCH")
		return
	}

	// Build keep set from request.
	keepSet := make(map[string]bool, len(body.BuildPriorities))
	for _, bp := range body.BuildPriorities {
		keepSet[bp.BuildID] = bp.Keep
	}

	// Validate priorities: for each conflicting part, sum claims of kept builds <= actualQty.
	// Build a map of lineItemID → actualQty from the token, and partID → actualQty from allocations.
	// We need to re-derive partID → actualQty from the token + current DB state.
	// Fetch current line items to get partID for each token item.
	ptItems, err := h.Queries.GetOrderLineItemsWithPartID(ctx, orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get line items", "INTERNAL")
		return
	}
	itemsByID := make(map[string]dbgen.GetOrderLineItemsWithPartIDRow, len(ptItems))
	for _, li := range ptItems {
		itemsByID[li.ID.String()] = li
	}

	// Build partID → actualQty from token items that are reductions.
	partActualQty := make(map[string]int32)
	for _, ti := range payload.LineItems {
		li, ok := itemsByID[ti.LineItemID]
		if !ok {
			continue
		}
		if ti.ActualQty < ti.PlannedQty {
			partActualQty[li.PartID.String()] = ti.ActualQty
		}
	}

	// For each reduced part, sum claims of kept builds.
	for partIDStr, actualQty := range partActualQty {
		var partID pgtype.UUID
		_ = partID.Scan(partIDStr)
		allocs, err := h.Queries.GetLinkedBuildAllocationsForOrderPart(ctx, dbgen.GetLinkedBuildAllocationsForOrderPartParams{
			OrderID: orderID,
			PartID:  partID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to validate priorities", "INTERNAL")
			return
		}
		var claimedByKept int32
		for _, a := range allocs {
			if keepSet[a.BuildID.String()] {
				claim := a.QuantityNeeded - a.QuantityReserved - a.QuantityReceived
				if claim < 0 {
					claim = 0
				}
				claimedByKept += claim
			}
		}
		if claimedByKept > actualQty {
			writeError(w, http.StatusUnprocessableEntity, "kept builds exceed available quantity", "INVALID_PRIORITIES")
			return
		}
	}

	// Begin transaction.
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := h.Queries.WithTx(tx)

	// Update line item quantities from token.
	for _, ti := range payload.LineItems {
		li, ok := itemsByID[ti.LineItemID]
		if !ok {
			continue
		}
		effectiveQty := ti.ActualQty
		if effectiveQty < li.QuantityReceived {
			effectiveQty = li.QuantityReceived
		}
		if effectiveQty < 1 {
			effectiveQty = 1 // quantity_ordered > 0 constraint
		}
		if err := qtx.SetOrderLineItemQuantityOrdered(ctx, dbgen.SetOrderLineItemQuantityOrderedParams{
			QuantityOrdered: effectiveQty,
			ID:              li.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update line item qty", "INTERNAL_ERROR")
			return
		}
		var priceNum pgtype.Numeric
		_ = priceNum.Scan(ti.ActualPrice)
		if err := qtx.UpdateOrderLineItemExternalData(ctx, dbgen.UpdateOrderLineItemExternalDataParams{
			ID:                     li.ID,
			UnitPriceSnapshot:      priceNum,
			ExternalUnitPrice:      priceNum,
			ExternalQtyOrdered:     &ti.ActualQty,
			ExternalQtyShipped:     &ti.ActualShipped,
			ExternalQtyBackordered: &ti.ActualBackorder,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update external data", "INTERNAL_ERROR")
			return
		}
	}

	// Revert deselected builds to pending and remove their build_orders links.
	linkedBuildIDs, err := qtx.GetLinkedBuildIDsForOrder(ctx, orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get linked builds", "INTERNAL_ERROR")
		return
	}
	linkedSet := make(map[string]bool, len(linkedBuildIDs))
	for _, bid := range linkedBuildIDs {
		linkedSet[bid.String()] = true
	}

	for _, bp := range body.BuildPriorities {
		if bp.Keep || !linkedSet[bp.BuildID] {
			continue
		}
		var buildID pgtype.UUID
		if err := buildID.Scan(bp.BuildID); err != nil {
			continue
		}
		// Only revert ordered/ready builds.
		if err := qtx.UpdateBuildStatus(ctx, dbgen.UpdateBuildStatusParams{
			ID:     buildID,
			Status: "pending",
			TeamID: teamID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revert build", "INTERNAL_ERROR")
			return
		}
		if err := qtx.DeleteBuildOrderLink(ctx, dbgen.DeleteBuildOrderLinkParams{
			BuildID: buildID,
			OrderID: orderID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to unlink build from order", "INTERNAL_ERROR")
			return
		}
	}

	// Recompute cart shortfalls.
	cart, err := qtx.GetOrCreateCart(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get cart", "INTERNAL_ERROR")
		return
	}
	if _, err := h.recomputeBomShortfalls(ctx, qtx, cart.ID, teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to recompute shortfalls", "INTERNAL_ERROR")
		return
	}

	// Link order to DigiKey reference.
	if _, err := qtx.LinkOrderExternalReference(ctx, dbgen.LinkOrderExternalReferenceParams{
		ExternalReference: &payload.SalesOrderID,
		ID:                orderID,
		TeamID:            teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link order", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Enqueue trimmed job to handle extra DigiKey SKUs not in partrack.
	jobID := h.Worker.Enqueue(worker.JobOrderLink, teamID.String(), map[string]any{
		"sales_order_id": payload.SalesOrderID,
		"order_id":       orderIDStr,
		"distributor_id": order.DistributorID.String(),
		"user_id":        user.ID,
		"skip_reconcile": true,
	})

	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{"order_id": orderIDStr})

	writeJSON(w, http.StatusOK, map[string]string{"job_id": jobID})
}

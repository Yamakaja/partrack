package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/scanner"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const scanSessionTTL = 2 * time.Hour

// scanSessionFromContext looks up the active scan session for the authenticated
// user. Scan tokens carry no team role, so the session record itself is the
// source of team_id and context. Returns nil if the user has no active session.
func (h *Handler) scanSessionFromContext(r *http.Request) (*dbgen.ScanSession, error) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		return nil, nil //nolint:nilnil
	}
	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		return nil, err
	}
	sess, err := h.Queries.GetActiveScanSessionForUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}
		return nil, err
	}
	return &sess, nil
}

// generateScanToken returns a cryptographically random token with the "scan_" prefix.
func generateScanToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "scan_" + hex.EncodeToString(b), nil
}

// ── Desktop-facing endpoints ───────────────────────────────────────────────

// CreateScanSession handles POST /api/v1/teams/{teamId}/scan-sessions.
// Creates a short-lived scan session and returns a token for the QR code.
// If the user already has an active session, returns 409 with the session ID.
func (h *Handler) CreateScanSession(w http.ResponseWriter, r *http.Request) {
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
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(body.OrderID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order_id", "BAD_REQUEST")
		return
	}

	// Check for an existing active session for this user.
	existing, err := h.Queries.GetActiveScanSessionForUser(r.Context(), userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err == nil {
		// Return 409 so the frontend can warn the user.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":               "active scan session already exists",
			"code":                "SCAN_SESSION_EXISTS",
			"existing_session_id": existing.ID.String(),
		})
		return
	}

	// Fetch the order to get a human-readable reference for the phone display.
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

	// Build a short display reference for the phone.
	orderRef := orderID.String()[:8]
	if order.PurchaseOrderNumber != nil && *order.PurchaseOrderNumber != "" {
		orderRef = *order.PurchaseOrderNumber
	} else if order.ExternalReference != nil && *order.ExternalReference != "" {
		orderRef = *order.ExternalReference
	}

	rawToken, err := generateScanToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	tokenHash := hashToken(rawToken)

	contextType := "receive_order"
	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(scanSessionTTL)) //nolint:errcheck

	sess, err := h.Queries.CreateScanSession(r.Context(), dbgen.CreateScanSessionParams{
		TokenHash:       tokenHash,
		UserID:          userID,
		TeamID:          teamID,
		ContextType:     &contextType,
		ContextOrderID:  orderID,
		ContextOrderRef: &orderRef,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         sess.ID.String(),
		"token":      rawToken,
		"expires_at": sess.ExpiresAt.Time.UTC().Format(time.RFC3339),
	})
}

// DeleteScanSession handles DELETE /api/v1/teams/{teamId}/scan-sessions/{sessionId}.
func (h *Handler) DeleteScanSession(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var sessionID pgtype.UUID
	if err := sessionID.Scan(chi.URLParam(r, "sessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id", "BAD_REQUEST")
		return
	}

	if err := h.Queries.DeleteScanSession(r.Context(), dbgen.DeleteScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Phone-facing endpoints ─────────────────────────────────────────────────

// GetScanSession handles GET /api/v1/scan/session.
// Phone polls this every 2s. On the first call, marks phone_connected and
// fires the scan_session_changed SSE event to notify the desktop.
func (h *Handler) GetScanSession(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromContext(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	// On the first call, mark phone connected and publish SSE to the desktop.
	if !sess.PhoneConnected {
		if sess.PhoneConnected {
			// Race: already set by a concurrent request — treat as already connected.
		} else {
			updated, err := h.Queries.SetScanSessionPhoneConnected(r.Context(), sess.ID)
			if err == nil {
				sess.PhoneConnected = updated.PhoneConnected
				// Notify the desktop.
				data, _ := json.Marshal(map[string]any{
					"session_id":      sess.ID.String(),
					"phone_connected": true,
				})
				h.Worker.Publish(worker.Event{
					Type:         "scan_session_changed",
					TeamID:       sess.TeamID.String(),
					TargetUserID: sess.UserID.String(),
					Data:         data,
				})
			}
		}
	}

	resp := map[string]any{
		"session_id":         sess.ID.String(),
		"context_type":       sess.ContextType,
		"context_order_id":   nil,
		"context_order_ref":  nil,
		"pending_scan":       nil,
	}
	if sess.ContextOrderID.Valid {
		resp["context_order_id"] = sess.ContextOrderID.String()
	}
	if sess.ContextOrderRef != nil {
		resp["context_order_ref"] = *sess.ContextOrderRef
	}
	if len(sess.PendingScan) > 0 {
		resp["pending_scan"] = json.RawMessage(sess.PendingScan)
	}
	writeJSON(w, http.StatusOK, resp)
}

// pendingScan is the JSON shape stored in scan_sessions.pending_scan.
type pendingScan struct {
	LineItemID      string `json:"line_item_id"`
	PartName        string `json:"part_name"`
	MPN             string `json:"mpn"`
	SKU             string `json:"sku"`
	BarcodeQty      int    `json:"barcode_qty"`
	QuantityOrdered int32  `json:"quantity_ordered"`
	AlreadyReceived int32  `json:"already_received"`
	OverReceive     bool   `json:"over_receive"`
}

// SubmitScan handles POST /api/v1/scan/scans.
// Phone submits a raw barcode string. The backend parses and matches it to an
// order line item, stores the result as pending_scan, and returns it.
func (h *Handler) SubmitScan(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromContext(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if sess.ContextType == nil || *sess.ContextType != "receive_order" || !sess.ContextOrderID.Valid {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "no active context",
			"code":  "NO_CONTEXT",
		})
		return
	}

	var body struct {
		Barcode string `json:"barcode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Barcode) == "" {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	// Parse the barcode.
	parsed, parseErr := scanner.Parse(body.Barcode)
	if parseErr != nil {
		if errors.Is(parseErr, scanner.ErrUnknownFormat) || errors.Is(parseErr, scanner.ErrParseFailure) {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "parse_error",
				"raw":    body.Barcode,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Fetch all line items for the order to match against.
	items, err := h.Queries.GetOrderLineItemsForScan(r.Context(), sess.ContextOrderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Match: SKU exact first, then MPN with largest-remaining tiebreaker.
	var matched *dbgen.GetOrderLineItemsForScanRow

	// SKU match.
	if parsed.SKU != "" {
		for i := range items {
			if strings.EqualFold(items[i].Sku, parsed.SKU) {
				matched = &items[i]
				break
			}
		}
	}

	// MPN fallback.
	if matched == nil && parsed.MPN != "" {
		var best *dbgen.GetOrderLineItemsForScanRow
		var bestRemaining int32
		for i := range items {
			itemMPN := ""
			if items[i].PartMpn != nil {
				itemMPN = *items[i].PartMpn
			}
			if strings.EqualFold(itemMPN, parsed.MPN) {
				remaining := items[i].QuantityOrdered - items[i].QuantityReceived
				if best == nil || remaining > bestRemaining {
					best = &items[i]
					bestRemaining = remaining
				}
			}
		}
		matched = best
	}

	if matched == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "no_match",
			"parsed_mpn": parsed.MPN,
			"parsed_sku": parsed.SKU,
		})
		return
	}

	remaining := matched.QuantityOrdered - matched.QuantityReceived
	overReceive := int32(parsed.Quantity) > remaining

	partName := ""
	if matched.PartDescription != nil {
		partName = *matched.PartDescription
	}
	partMPN := ""
	if matched.PartMpn != nil {
		partMPN = *matched.PartMpn
	}

	ps := pendingScan{
		LineItemID:      matched.ID.String(),
		PartName:        partName,
		MPN:             partMPN,
		SKU:             matched.Sku,
		BarcodeQty:      parsed.Quantity,
		QuantityOrdered: matched.QuantityOrdered,
		AlreadyReceived: matched.QuantityReceived,
		OverReceive:     overReceive,
	}

	psBytes, err := json.Marshal(ps)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if _, err := h.Queries.SetScanSessionPendingScan(r.Context(), dbgen.SetScanSessionPendingScanParams{
		ID:          sess.ID,
		PendingScan: psBytes,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "matched",
		"line_item_id":     ps.LineItemID,
		"part_name":        ps.PartName,
		"mpn":              ps.MPN,
		"sku":              ps.SKU,
		"barcode_qty":      ps.BarcodeQty,
		"quantity_ordered": ps.QuantityOrdered,
		"already_received": ps.AlreadyReceived,
		"over_receive":     ps.OverReceive,
	})
}

// ConfirmScan handles POST /api/v1/scan/confirm.
// Applies the pending_scan receive action and clears the pending scan.
func (h *Handler) ConfirmScan(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromContext(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if len(sess.PendingScan) == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "no pending scan",
			"code":  "NO_PENDING_SCAN",
		})
		return
	}

	var ps pendingScan
	if err := json.Unmarshal(sess.PendingScan, &ps); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var body struct {
		Qty *int32 `json:"qty"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	qty := int32(ps.BarcodeQty)
	if body.Qty != nil && *body.Qty > 0 {
		qty = *body.Qty
	}
	if qty <= 0 {
		writeError(w, http.StatusBadRequest, "qty must be positive", "BAD_REQUEST")
		return
	}

	var lineItemID pgtype.UUID
	if err := lineItemID.Scan(ps.LineItemID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	teamID := sess.TeamID
	orderID := sess.ContextOrderID

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

	// Fetch line item for part_id and inventory entry.
	li, err := h.Queries.GetOrderLineItem(r.Context(), dbgen.GetOrderLineItemParams{
		ID:     lineItemID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "line item not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Guard: order must be receivable.
	order, err := h.Queries.GetOrder(r.Context(), dbgen.GetOrderParams{
		ID:     orderID,
		TeamID: teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if order.Status != dbgen.OrderStatusPlaced && order.Status != dbgen.OrderStatusPartiallyReceived {
		writeError(w, http.StatusConflict, "order is not in a receivable state", "ORDER_NOT_RECEIVABLE")
		return
	}

	// Get or create inventory entry.
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

	updatedLI, err := qtx.ReceiveOrderLineItem(r.Context(), dbgen.ReceiveOrderLineItemParams{
		ID:               lineItemID,
		QuantityReceived: qty,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	reason := fmt.Sprintf("Received from order %s (scan)", orderID.String())
	if _, err := qtx.CreateOrderStockAdjustment(r.Context(), dbgen.CreateOrderStockAdjustmentParams{
		InventoryEntryID: invEntry.ID,
		Delta:            qty,
		Reason:           &reason,
		CreatedBy:        userID,
		OrderLineItemID:  lineItemID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	newQty, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
		ID:             invEntry.ID,
		QuantityOnHand: qty,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Determine order status transition.
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

	// Allocate to linked builds (same logic as ReceiveOrderItem).
	linkedBuildIDs, err := qtx.GetLinkedBuildIDsForOrder(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	allocs, err := qtx.GetLinkedBuildAllocationsForOrderPart(r.Context(), dbgen.GetLinkedBuildAllocationsForOrderPartParams{
		OrderID: orderID,
		PartID:  li.PartID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	allocByBuild := make(map[string]dbgen.GetLinkedBuildAllocationsForOrderPartRow, len(allocs))
	for _, a := range allocs {
		allocByBuild[a.BuildID.String()] = a
	}
	var buildIDsToCheckReady []pgtype.UUID
	remaining := qty
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
			buildIDsToCheckReady = append(buildIDsToCheckReady, bid)
		}
	}
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

	// Clear pending_scan.
	h.Queries.ClearScanSessionPendingScan(r.Context(), sess.ID) //nolint:errcheck

	// Publish SSE events (team-wide, skip the acting user — but scan tokens
	// have no normal SSE connection, so all desktop users get the event).
	teamIDStr := teamID.String()
	d, _ := json.Marshal(map[string]string{"order_id": orderID.String()})
	h.Worker.Publish(worker.Event{Type: "order_changed", TeamID: teamIDStr, Data: d})
	d2, _ := json.Marshal(map[string]string{"part_id": li.PartID.String(), "entry_id": invEntry.ID.String()})
	h.Worker.Publish(worker.Event{Type: "inventory_changed", TeamID: teamIDStr, Data: d2})
	for _, bid := range readyBuildIDs {
		_ = h.Queries.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
			ID:     bid,
			Status: "ready",
			TeamID: teamID,
		})
		d3, _ := json.Marshal(map[string]string{"build_id": bid.String()})
		h.Worker.Publish(worker.Event{Type: "build_changed", TeamID: teamIDStr, Data: d3})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"new_quantity_received": updatedLI.QuantityReceived,
		"new_quantity_on_hand":  newQty,
		"order_status":         string(newOrderStatus),
	})
}

// SkipScan handles POST /api/v1/scan/skip.
// Clears the pending scan without applying it.
func (h *Handler) SkipScan(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromContext(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	h.Queries.ClearScanSessionPendingScan(r.Context(), sess.ID) //nolint:errcheck
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/scanner"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const scanSessionTTL = 30 * 24 * time.Hour

// scanSessionFromScanToken looks up the active scan session by raw scan token
// (sent as ?token= query param by EventSource, or Authorization: Bearer by fetch).
// Returns (nil, nil) if the token doesn't match any live session.
func (h *Handler) scanSessionFromScanToken(r *http.Request) (*dbgen.ScanSession, error) {
	rawToken := ""
	// Try query param first (EventSource can't set headers).
	if t := r.URL.Query().Get("token"); strings.HasPrefix(t, "scan_") {
		rawToken = t
	} else {
		// Fall back to Authorization: Bearer.
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			rawToken = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if rawToken == "" {
		return nil, nil //nolint:nilnil
	}
	hash := hashToken(rawToken)
	row, err := h.Queries.GetScanSessionByTokenHash(r.Context(), hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}
		return nil, err
	}
	sess := dbgen.ScanSession{
		ID:             row.ID,
		TokenHash:      row.TokenHash,
		UserID:         row.UserID,
		TeamID:         row.TeamID,
		PhoneConnected: row.PhoneConnected,
		PendingScan:    row.PendingScan,
		CreatedAt:      row.CreatedAt,
		ExpiresAt:      row.ExpiresAt,
	}
	return &sess, nil
}

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

// buildOrderRef returns a short human-readable reference for an order.
func buildOrderRef(id pgtype.UUID, purchaseOrderNumber, externalReference *string) string {
	if purchaseOrderNumber != nil && *purchaseOrderNumber != "" {
		return *purchaseOrderNumber
	}
	if externalReference != nil && *externalReference != "" {
		return *externalReference
	}
	return id.String()[:8]
}

// ── Desktop-facing endpoints ───────────────────────────────────────────────

// CreateScanSession handles POST /api/v1/teams/{teamId}/scan-sessions.
// Creates a long-lived scan session and returns a token for the QR code.
// order_id is optional — when absent the phone starts in menu mode.
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

	// Check for an existing active session for this user.
	existing, err := h.Queries.GetActiveScanSessionForUser(r.Context(), userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":               "active scan session already exists",
			"code":                "SCAN_SESSION_EXISTS",
			"existing_session_id": existing.ID.String(),
		})
		return
	}

	rawToken, err := generateScanToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	tokenHash := hashToken(rawToken)

	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(scanSessionTTL)) //nolint:errcheck

	sess, err := h.Queries.CreateScanSession(r.Context(), dbgen.CreateScanSessionParams{
		TokenHash: tokenHash,
		UserID:    userID,
		TeamID:    teamID,
		ExpiresAt: expiresAt,
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

// TODO: Scan sessions are user bound. A user should be able to delete their own sessions!
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

// GetActiveScanSession handles GET /api/v1/teams/{teamId}/scan-sessions/active.
// Returns the user's active scan session plus live connection status.
func (h *Handler) GetActiveScanSession(w http.ResponseWriter, r *http.Request) {
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

	sess, err := h.Queries.GetActiveScanSessionForUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	sessionIDStr := sess.ID.String()
	liveConnected := h.ScanHub.IsConnected(sessionIDStr)
	ip, ua := h.ScanHub.PhoneInfo(sessionIDStr)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":              sessionIDStr,
		"phone_connected": sess.PhoneConnected && liveConnected,
		"phone_ip":        ip,
		"phone_ua":        ua,
	})
}

// PushScanContext handles PATCH /api/v1/teams/{teamId}/scan-sessions/{sessionId}/context.
// Desktop pushes an order context to the phone.
func (h *Handler) PushScanContext(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OrderID == "" {
		writeError(w, http.StatusBadRequest, "order_id is required", "BAD_REQUEST")
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(body.OrderID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order_id", "BAD_REQUEST")
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

	ref := buildOrderRef(order.ID, order.PurchaseOrderNumber, order.ExternalReference)

	// Push to phone over SSE — no DB write, context is ephemeral.
	msg, _ := json.Marshal(map[string]any{
		"type":              "scan_context_changed",
		"context_order_id":  order.ID.String(),
		"context_order_ref": ref,
	})
	h.ScanHub.Send(sessionID.String(), msg)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"context_order_ref": ref,
	})
}

// ── Phone-facing endpoints ─────────────────────────────────────────────────

// StreamScanSession handles GET /api/v1/scan/stream.
// Phone maintains this SSE connection; server detects disconnect and notifies desktop.
// Token is read from ?token= query param (EventSource can't set headers).
func (h *Handler) StreamScanSession(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Mark phone connected in DB and publish to desktop.
	if _, dbErr := h.Queries.SetScanSessionPhoneConnected(r.Context(), sess.ID); dbErr == nil {
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

	// RemoteAddr is already the real client IP — the RealIP middleware rewrote it.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	ua := r.Header.Get("User-Agent")
	ch, cleanup := h.ScanHub.Register(sess.ID.String(), ip, ua)
	defer func() {
		cleanup()
		// Mark disconnected in DB and notify desktop.
		h.Queries.SetScanSessionPhoneDisconnected(r.Context(), sess.ID) //nolint:errcheck
		data, _ := json.Marshal(map[string]any{
			"session_id":      sess.ID.String(),
			"phone_connected": false,
		})
		h.Worker.Publish(worker.Event{
			Type:         "scan_session_changed",
			TeamID:       sess.TeamID.String(),
			TargetUserID: sess.UserID.String(),
			Data:         data,
		})
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported", "INTERNAL_ERROR")
		return
	}

	// Send initial heartbeat.
	fmt.Fprintf(w, ": ping\n\n") //nolint:errcheck
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg) //nolint:errcheck
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n") //nolint:errcheck
			flusher.Flush()
		}
	}
}

// GetScanSession handles GET /api/v1/scan/session.
// Phone polls this every few seconds as a fallback / context recovery.
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
		updated, err := h.Queries.SetScanSessionPhoneConnected(r.Context(), sess.ID)
		if err == nil {
			sess.PhoneConnected = updated.PhoneConnected
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

	resp := map[string]any{
		"session_id":   sess.ID.String(),
		"pending_scan": nil,
	}
	if len(sess.PendingScan) > 0 {
		resp["pending_scan"] = json.RawMessage(sess.PendingScan)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListScanOrders handles GET /api/v1/scan/orders.
// Returns receivable orders for the phone to pick from.
func (h *Handler) ListScanOrders(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	orders, err := h.Queries.ListReceivableOrdersForTeam(r.Context(), sess.TeamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type orderItem struct {
		ID                 string  `json:"id"`
		Title              string  `json:"title"`               // PO number, or UUID prefix as fallback
		TitleIsFallback    bool    `json:"title_is_fallback"`   // true when showing UUID prefix
		ExternalRef        *string `json:"external_ref"`        // distributor order ID, if any
		Status             string  `json:"status"`
		DistributorName    string  `json:"distributor_name"`
		ItemCount          int32   `json:"item_count"`
		ItemsFullyReceived int32   `json:"items_fully_received"`
		TotalQtyOrdered    int32   `json:"total_qty_ordered"`
		TotalQtyReceived   int32   `json:"total_qty_received"`
		TotalValue         *string `json:"total_value"`
		CreatedByName      string  `json:"created_by_name"`
		CreatedByMe        bool    `json:"created_by_me"`
		CreatedAt          string  `json:"created_at"`
	}
	items := make([]orderItem, len(orders))
	for i, o := range orders {
		title := o.ID.String()[:8]
		isFallback := true
		if o.PurchaseOrderNumber != nil && *o.PurchaseOrderNumber != "" {
			title = *o.PurchaseOrderNumber
			isFallback = false
		}
		var extRef *string
		if o.ExternalReference != nil && *o.ExternalReference != "" {
			extRef = o.ExternalReference
		}
		items[i] = orderItem{
			ID:                 o.ID.String(),
			Title:              title,
			TitleIsFallback:    isFallback,
			ExternalRef:        extRef,
			Status:             string(o.Status),
			DistributorName:    o.DistributorName,
			ItemCount:          o.ItemCount,
			ItemsFullyReceived: o.ItemsFullyReceived,
			TotalQtyOrdered:    o.TotalQtyOrdered,
			TotalQtyReceived:   o.TotalQtyReceived,
			TotalValue:         numericString(o.TotalValue),
			CreatedByName:      o.CreatedByName,
			CreatedByMe:        o.CreatedBy == sess.UserID,
			CreatedAt:          o.CreatedAt.Time.UTC().Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": items})
}

// SearchScanParts handles GET /api/v1/scan/parts?q=.
// Returns parts matching the search query for the phone's Find Part flow.
func (h *Handler) SearchScanParts(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var qPtr *string
	if q != "" {
		qPtr = &q
	}

	parts, err := h.Queries.ListPartsWithInventory(r.Context(), dbgen.ListPartsWithInventoryParams{
		TeamID: sess.TeamID,
		Q:      qPtr,
		Limit:  20,
		Offset: 0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type partItem struct {
		ID               string  `json:"id"`
		MPN              *string `json:"mpn"`
		Description      *string `json:"description"`
		InventoryEntryID *string `json:"inventory_entry_id,omitempty"`
		QuantityOnHand   *int32  `json:"quantity_on_hand,omitempty"`
		StorageLocation  *string `json:"storage_location,omitempty"`
	}
	items := make([]partItem, len(parts))
	for i, p := range parts {
		desc := p.Description
		if desc == nil {
			desc = p.ShortDescription
		}
		item := partItem{
			ID:          p.ID.String(),
			MPN:         p.Mpn,
			Description: desc,
		}
		if p.InventoryEntryID.Valid {
			s := p.InventoryEntryID.String()
			item.InventoryEntryID = &s
			item.QuantityOnHand = p.QuantityOnHand
			if p.StorageLocationLabel != nil {
				item.StorageLocation = p.StorageLocationLabel
			}
		}
		items[i] = item
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": items})
}

// CreateScanStockAdjustment handles POST /api/v1/scan/stock-adjustment.
// Applies a manual stock delta from the phone.
func (h *Handler) CreateScanStockAdjustment(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	// Role enforcement: member+ required for stock adjustments.
	membership, err := h.checkTeamMembership(r.Context(), sess.TeamID, sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if membership == nil || !roleAtLeast(membership.Role, "member") {
		writeError(w, http.StatusForbidden, "member role required", "FORBIDDEN")
		return
	}

	var body struct {
		InventoryEntryID string `json:"inventory_entry_id"`
		Delta            int32  `json:"delta"`
		Reason           string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.InventoryEntryID == "" {
		writeError(w, http.StatusBadRequest, "inventory_entry_id is required", "BAD_REQUEST")
		return
	}
	if body.Delta == 0 {
		writeError(w, http.StatusBadRequest, "delta must not be zero", "BAD_REQUEST")
		return
	}

	var entryID pgtype.UUID
	if err := entryID.Scan(body.InventoryEntryID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid inventory_entry_id", "BAD_REQUEST")
		return
	}

	// Verify the entry belongs to this team.
	entry, err := h.Queries.GetInventoryEntry(r.Context(), dbgen.GetInventoryEntryParams{
		ID:     entryID,
		TeamID: sess.TeamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "inventory entry not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	reason := body.Reason
	if reason == "" {
		reason = "Manual scan adjustment"
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.CreateStockAdjustment(r.Context(), dbgen.CreateStockAdjustmentParams{
		InventoryEntryID: entryID,
		Delta:            body.Delta,
		Reason:           &reason,
		CreatedBy:        sess.UserID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	newQty, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
		ID:             entryID,
		QuantityOnHand: body.Delta,
	})
	if err != nil {
		// Likely a check constraint violation (stock went negative).
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "stock adjustment would result in negative quantity",
			"code":  "NEGATIVE_STOCK",
		})
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Publish SSE so desktop inventory updates.
	d, _ := json.Marshal(map[string]string{
		"part_id":  entry.PartID.String(),
		"entry_id": entryID.String(),
	})
	h.Worker.Publish(worker.Event{Type: "inventory_changed", TeamID: sess.TeamID.String(), Data: d})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"new_quantity_on_hand": newQty,
	})
}

// LookupScanBarcode handles POST /api/v1/scan/lookup.
// Parses a bag barcode and looks up the part by SKU (scoped to identified
// distributor when possible) or MPN. Returns the part with inventory info,
// or the parsed barcode fields when the part is not found.
func (h *Handler) LookupScanBarcode(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	var body struct {
		Barcode string `json:"barcode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Barcode) == "" {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	parsed, parseErr := scanner.Parse(body.Barcode)
	if parseErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "parse_error",
			"raw":    body.Barcode,
		})
		return
	}

	// no_fields: barcode parsed but no identifiable part information.
	if parsed.SKU == "" && parsed.MPN == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":             "no_fields",
			"parsed_mpn":         parsed.MPN,
			"parsed_sku":         parsed.SKU,
			"parsed_distributor": parsed.Distributor,
			"parsed_qty":         parsed.Quantity,
			"parsed_lot":         parsed.Lot,
		})
		return
	}

	// Try SKU lookup — prefer distributor-scoped when we identified the source.
	if parsed.SKU != "" {
		if parsed.Distributor != "" {
			row, err := h.Queries.GetPartBySKUAndDistributorWithInventory(r.Context(), dbgen.GetPartBySKUAndDistributorWithInventoryParams{
				Sku:             parsed.SKU,
				TeamID:          sess.TeamID,
				DistributorName: parsed.Distributor,
			})
			if err == nil {
				h.writeLookupFound(w, r, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
					&row.Sku, &row.DistributorName, row.InventoryEntryID, row.QuantityOnHand, row.StorageLocationLabel, parsed)
				return
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}

		// Fallback: SKU across all distributors.
		row, err := h.Queries.GetPartBySKUWithInventory(r.Context(), dbgen.GetPartBySKUWithInventoryParams{
			Sku:    parsed.SKU,
			TeamID: sess.TeamID,
		})
		if err == nil {
			h.writeLookupFound(w, r, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
				&row.Sku, &row.DistributorName, row.InventoryEntryID, row.QuantityOnHand, row.StorageLocationLabel, parsed)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	// Fallback: MPN lookup.
	if parsed.MPN != "" {
		row, err := h.Queries.GetPartByMPNWithInventory(r.Context(), dbgen.GetPartByMPNWithInventoryParams{
			Mpn:    &parsed.MPN,
			TeamID: sess.TeamID,
		})
		if err == nil {
			h.writeLookupFound(w, r, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
				nil, nil, row.InventoryEntryID, row.QuantityOnHand, row.StorageLocationLabel, parsed)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	// not_found_in_db: part info extracted but not in catalog.
	// Resolve distributor UUIDs from team's enabled configs so the frontend
	// knows whether it can auto-import or search a distributor.
	configs, _ := h.Queries.ListEnabledDistributorConfigsForTeam(r.Context(), sess.TeamID)
	var parsedDistributorID, preferredDistributorID, preferredDistributorName *string
	for _, c := range configs {
		cCopy := c
		if cCopy.Preferred && preferredDistributorID == nil {
			id := cCopy.DistributorID.String()
			preferredDistributorID = &id
			name := cCopy.DistributorName
			preferredDistributorName = &name
		}
		if parsed.Distributor != "" && parsedDistributorID == nil {
			lo := strings.ToLower(cCopy.DistributorName)
			lp := strings.ToLower(parsed.Distributor)
			if strings.Contains(lo, lp) || strings.Contains(lp, lo) {
				id := cCopy.DistributorID.String()
				parsedDistributorID = &id
			}
		}
	}

	canAutoImport := parsedDistributorID != nil && parsed.SKU != ""

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                   "not_found_in_db",
		"parsed_mpn":               parsed.MPN,
		"parsed_manufacturer":      parsed.Manufacturer,
		"parsed_sku":               parsed.SKU,
		"parsed_distributor":       parsed.Distributor,
		"parsed_distributor_id":    parsedDistributorID,
		"preferred_distributor_id": preferredDistributorID,
		"preferred_distributor_name": preferredDistributorName,
		"parsed_qty":               parsed.Quantity,
		"parsed_lot":               parsed.Lot,
		"can_auto_import":          canAutoImport,
	})
}

// writeLookupFound writes the "found" response for LookupScanBarcode,
// auto-creating the team's inventory entry at quantity 0 if one does not exist.
func (h *Handler) writeLookupFound(w http.ResponseWriter, r *http.Request,
	teamID pgtype.UUID, partID pgtype.UUID, mpn, manufacturer, desc, shortDesc *string,
	sku, distributorName *string, invEntryID pgtype.UUID, qoh *int32, storageLabel *string,
	parsed *scanner.ParsedBarcode) {

	newlyCreated := false
	finalEntryID := invEntryID
	finalQoh := qoh
	finalStorageLabel := storageLabel

	if !invEntryID.Valid {
		entry, err := h.Queries.CreateInventoryEntry(r.Context(), dbgen.CreateInventoryEntryParams{
			TeamID: teamID,
			PartID: partID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		finalEntryID = entry.ID
		var zero int32 = 0
		finalQoh = &zero
		finalStorageLabel = nil
		newlyCreated = true
	}

	d := desc
	if d == nil {
		d = shortDesc
	}
	resp := map[string]any{
		"status":                        "found",
		"id":                            partID.String(),
		"mpn":                           mpn,
		"manufacturer":                  manufacturer,
		"description":                   d,
		"barcode_qty":                   parsed.Quantity,
		"distributor":                   parsed.Distributor,
		"inventory_entry_id":            finalEntryID.String(),
		"quantity_on_hand":              finalQoh,
		"storage_location":              finalStorageLabel,
		"inventory_entry_newly_created": newlyCreated,
		"bag_qty":                       parsed.Quantity,
	}
	if sku != nil {
		resp["sku"] = *sku
	}
	if distributorName != nil {
		resp["distributor_name"] = *distributorName
	}
	writeJSON(w, http.StatusOK, resp)
}

// pendingScan is the JSON shape stored in scan_sessions.pending_scan.
type pendingScan struct {
	OrderID         string `json:"order_id"`
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
	var body struct {
		OrderID string `json:"order_id"`
		Barcode string `json:"barcode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Barcode) == "" || body.OrderID == "" {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var orderID pgtype.UUID
	if err := orderID.Scan(body.OrderID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order_id", "BAD_REQUEST")
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
	items, err := h.Queries.GetOrderLineItemsForScan(r.Context(), orderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Match: SKU exact first, then MPN with largest-remaining tiebreaker.
	var matched *dbgen.GetOrderLineItemsForScanRow

	if parsed.SKU != "" {
		for i := range items {
			if strings.EqualFold(items[i].Sku, parsed.SKU) {
				matched = &items[i]
				break
			}
		}
	}

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
		OrderID:         body.OrderID,
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
	var orderID pgtype.UUID
	if err := orderID.Scan(ps.OrderID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	teamID := sess.TeamID

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
		"ok":                    true,
		"new_quantity_received": updatedLI.QuantityReceived,
		"new_quantity_on_hand":  newQty,
		"order_status":          string(newOrderStatus),
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

// GetScanPartDetail handles GET /api/v1/scan/parts/{partId}.
// Returns comprehensive part detail (identity, inventory, listings, parameters)
// for the phone's PartDetail screen.
func (h *Handler) GetScanPartDetail(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	partIDStr := chi.URLParam(r, "partId")
	var partID pgtype.UUID
	if err := partID.Scan(partIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part id", "BAD_REQUEST")
		return
	}

	ctx := r.Context()

	part, err := h.Queries.GetPartWithInventory(ctx, dbgen.GetPartWithInventoryParams{
		ID:     partID,
		TeamID: pgtype.UUID{Bytes: sess.TeamID.Bytes, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	listings, err := h.Queries.ListListings(ctx, partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rawParams, err := h.Queries.ListParameters(ctx, partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Deduplicate parameters by name, preferring user-specified over distributor-sourced.
	type paramItem struct {
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit,omitempty"`
	}
	seen := map[string]bool{}
	params := []paramItem{}
	// ListParameters is ordered source_type ASC — 'distributor' < 'user', so iterate
	// twice: first collect user params, then fill in distributor params not yet seen.
	for _, p := range rawParams {
		if p.SourceType == "user" {
			seen[p.Name] = true
			params = append(params, paramItem{Name: p.Name, Value: p.Value, Unit: p.Unit})
		}
	}
	for _, p := range rawParams {
		if p.SourceType != "user" && !seen[p.Name] {
			seen[p.Name] = true
			params = append(params, paramItem{Name: p.Name, Value: p.Value, Unit: p.Unit})
		}
	}

	type listingItem struct {
		ID              string  `json:"id"`
		DistributorName string  `json:"distributor_name"`
		SKU             string  `json:"sku"`
		PackagingType   string  `json:"packaging_type"`
		UnitPrice       *string `json:"unit_price,omitempty"`
		StockAvailable  *int32  `json:"stock_available,omitempty"`
		MOQ             int32   `json:"moq"`
	}
	listingItems := make([]listingItem, len(listings))
	for i, l := range listings {
		listingItems[i] = listingItem{
			ID:              l.ID.String(),
			DistributorName: l.DistributorName,
			SKU:             l.Sku,
			PackagingType:   l.PackagingType,
			UnitPrice:       numericString(l.UnitPrice),
			StockAvailable:  l.StockAvailable,
			MOQ:             l.Moq,
		}
	}

	desc := part.Description
	if desc == nil {
		desc = part.ShortDescription
	}

	var invEntryID *string
	if part.InventoryEntryID.Valid {
		s := part.InventoryEntryID.String()
		invEntryID = &s
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                     part.ID.String(),
		"mpn":                    part.Mpn,
		"manufacturer":           part.Manufacturer,
		"description":            desc,
		"package":                part.Package,
		"lifecycle_status":       string(part.LifecycleStatus),
		"datasheet_url":          part.DatasheetUrl,
		"product_url":            part.ProductUrl,
		"image_url":              part.ImageUrl,
		"category_name":          part.CategoryName,
		"inventory_entry_id":     invEntryID,
		"quantity_on_hand":       part.QuantityOnHand,
		"storage_location":       part.StorageLocationLabel,
		"minimum_stock_quantity": part.MinimumStockQuantity,
		"reorder_quantity":       part.ReorderQuantity,
		"listings":               listingItems,
		"parameters":             params,
	})
}

// roleAtLeast returns true if role meets the minimum required role level.
func roleAtLeast(role, minRole string) bool {
	order := map[string]int{"viewer": 0, "member": 1, "purchaser": 2, "admin": 3}
	return order[role] >= order[minRole]
}

// SearchScanDistributor handles GET /api/v1/scan/distributor-search
// Query params: distributor_id (required), q (required), limit (optional, default 20, max 50).
// Proxies a live distributor catalog search using the team's API credentials.
func (h *Handler) SearchScanDistributor(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	distributorIDStr := r.URL.Query().Get("distributor_id")
	q := r.URL.Query().Get("q")
	if distributorIDStr == "" || q == "" {
		writeError(w, http.StatusBadRequest, "distributor_id and q are required", "BAD_REQUEST")
		return
	}

	var distributorID pgtype.UUID
	if err := distributorID.Scan(distributorIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid distributor_id", "BAD_REQUEST")
		return
	}

	limit := int32(20)
	if s := r.URL.Query().Get("limit"); s != "" {
		var v int32
		if _, err := fmt.Sscanf(s, "%d", &v); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 50 {
		limit = 50
	}

	cfg, err := h.Queries.GetDistributorConfigForSearch(r.Context(), dbgen.GetDistributorConfigForSearchParams{
		TeamID:        sess.TeamID,
		DistributorID: distributorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "distributor config not found or not enabled", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if cfg.ApiType == nil {
		writeError(w, http.StatusUnprocessableEntity, "distributor has no API integration", "NO_API")
		return
	}

	client := h.Distributors.Lookup(*cfg.ApiType)
	if client == nil {
		writeError(w, http.StatusInternalServerError, "no client registered for distributor api_type", "INTERNAL_ERROR")
		return
	}

	result, err := client.SearchParts(r.Context(), distributor.SearchQuery{
		Keywords: q,
		Limit:    limit,
	}, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		writeError(w, http.StatusBadGateway, "search failed: "+err.Error(), "SEARCH_ERROR")
		return
	}

	type productResp struct {
		MPN            string `json:"mpn"`
		Manufacturer   string `json:"manufacturer"`
		Description    string `json:"description"`
		Category       string `json:"category"`
		PrimarySKU     string `json:"primary_sku"`
		Package        string `json:"package"`
		UnitPrice      string `json:"unit_price"`
		StockAvailable *int32 `json:"stock_available"`
	}

	if result == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"items":               []productResp{},
			"total":               0,
			"exact_match_indices": []int{},
		})
		return
	}

	items := make([]productResp, len(result.Products))
	for i, p := range result.Products {
		items[i] = productResp{
			MPN:            p.MPN,
			Manufacturer:   p.Manufacturer,
			Description:    p.Description,
			Category:       p.Category,
			PrimarySKU:     p.PrimarySKU,
			Package:        p.Package,
			UnitPrice:      p.UnitPrice,
			StockAvailable: p.StockAvailable,
		}
	}

	exactIndices := result.ExactMatchIndices
	if exactIndices == nil {
		exactIndices = []int{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":               items,
		"total":               result.TotalCount,
		"exact_match_indices": exactIndices,
	})
}

// ImportScanPart handles POST /api/v1/scan/parts/import
// Body: {distributor_id, sku} (both required). Returns 200 {part_id}.
// Synchronously imports the part from the distributor API, finds or creates the
// catalog entry, and ensures a team inventory entry exists.
func (h *Handler) ImportScanPart(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	var req struct {
		DistributorID string `json:"distributor_id"`
		SKU           string `json:"sku"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DistributorID == "" || req.SKU == "" {
		writeError(w, http.StatusBadRequest, "distributor_id and sku are required", "BAD_REQUEST")
		return
	}

	partID, err := h.importPartBySKU(r.Context(), sess.TeamID, req.DistributorID, req.SKU)
	if err != nil {
		h.writeImportError(w, "ImportScanPart", req.SKU, err)
		return
	}

	h.publishEvent(r, "part_changed", sess.TeamID.String(), map[string]string{"part_id": partID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"part_id": partID.String()})
}

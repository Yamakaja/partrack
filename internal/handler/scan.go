package handler

import (
	"context"
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

// DeleteScanSession handles DELETE /api/v1/teams/{teamId}/scan-sessions/{sessionId}.
// Purchasers may delete their own session; admins may delete any session in the team.
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

	sess, err := h.Queries.GetScanSession(r.Context(), dbgen.GetScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "scan session not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if sess.UserID != userID {
		membership, err := h.checkTeamMembership(r.Context(), teamID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if membership == nil || !roleAtLeast(membership.Role, "admin") {
			writeError(w, http.StatusForbidden, "can only delete your own scan session", "FORBIDDEN")
			return
		}
	}

	if err := h.Queries.DeleteScanSession(r.Context(), dbgen.DeleteScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.Cache.EvictScanTokenBySessionID(sessionID.String())
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
		"session_id":                 sess.ID.String(),
		"pending_scan":               nil,
		"active_kitting_session_id":  nil,
		"active_kitting_build_ref":   nil,
	}
	if len(sess.PendingScan) > 0 {
		resp["pending_scan"] = json.RawMessage(sess.PendingScan)
	}
	// Include active kitting session so the phone can resume after reconnect.
	if aks, err := h.Queries.GetActiveKittingSessionForScanSession(r.Context(), sess.ID); err == nil {
		resp["active_kitting_session_id"] = aks.ID.String()
		if ksInfo, err2 := h.Queries.GetKittingSession(r.Context(), aks.ID); err2 == nil {
			resp["active_kitting_build_ref"] = ksInfo.ProjectName + " " + ksInfo.BomRevisionLabel
		}
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
		Notes              *string `json:"notes"`
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
		var notes *string
		if o.Notes != nil && *o.Notes != "" {
			notes = o.Notes
		}
		items[i] = orderItem{
			ID:                 o.ID.String(),
			Title:              title,
			TitleIsFallback:    isFallback,
			ExternalRef:        extRef,
			Notes:              notes,
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

	type partLocation struct {
		ID         string  `json:"id"`
		Coordinate []int32 `json:"coordinate"`
	}
	type partItem struct {
		ID               string        `json:"id"`
		MPN              *string       `json:"mpn"`
		Description      *string       `json:"description"`
		InventoryEntryID *string       `json:"inventory_entry_id,omitempty"`
		QuantityOnHand   *int32        `json:"quantity_on_hand,omitempty"`
		StorageLocation  *partLocation `json:"storage_location,omitempty"`
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
			if p.StorageLocationID.Valid {
				item.StorageLocation = &partLocation{
					ID:         p.StorageLocationID.String(),
					Coordinate: p.StorageCoordinate,
				}
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

	// Audit log for stock_adjusted.
	delta := body.Delta
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "stock_adjusted",
		EntityType: strPtr("part"),
		EntityID:   entry.PartID,
		Quantity:   &delta,
		Metadata:   map[string]any{"reason": body.Reason, "inventory_entry_id": entryID.String()},
	})

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

	parsed, parseErr := scanner.ParseAny(body.Barcode)
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
				h.writeLookupFound(w, r, sess, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
					&row.Sku, &row.DistributorName, row.InventoryEntryID, row.QuantityOnHand,
					row.StorageLocationID, row.StorageCoordinate, parsed)
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
			h.writeLookupFound(w, r, sess, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
				&row.Sku, &row.DistributorName, row.InventoryEntryID, row.QuantityOnHand,
				row.StorageLocationID, row.StorageCoordinate, parsed)
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	// Fallback: MPN lookup. When a manufacturer is also present (e.g. from a
	// partrack part-level QR), try the narrower MPN+manufacturer query first to
	// avoid false matches on shared MPNs across manufacturers.
	if parsed.MPN != "" {
		if parsed.Manufacturer != "" {
			row, err := h.Queries.GetPartByMPNAndManufacturerWithInventory(r.Context(), dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
				Mpn:          &parsed.MPN,
				Manufacturer: &parsed.Manufacturer,
				TeamID:       sess.TeamID,
			})
			if err == nil {
				h.writeLookupFound(w, r, sess, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
					nil, nil, row.InventoryEntryID, row.QuantityOnHand,
					row.StorageLocationID, row.StorageCoordinate, parsed)
				return
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}

		row, err := h.Queries.GetPartByMPNWithInventory(r.Context(), dbgen.GetPartByMPNWithInventoryParams{
			Mpn:    &parsed.MPN,
			TeamID: sess.TeamID,
		})
		if err == nil {
			h.writeLookupFound(w, r, sess, sess.TeamID, row.ID, row.Mpn, row.Manufacturer, row.Description, row.ShortDescription,
				nil, nil, row.InventoryEntryID, row.QuantityOnHand,
				row.StorageLocationID, row.StorageCoordinate, parsed)
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

	type distConfigItem struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	distConfigs := make([]distConfigItem, 0, len(configs))
	for _, cfg := range configs {
		distConfigs = append(distConfigs, distConfigItem{
			ID:   cfg.DistributorID.String(),
			Name: cfg.DistributorName,
		})
	}

	// Audit log for part_not_found (barcode info present but part not in catalog).
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:           "part_not_found",
		ParsedSku:           strPtr(parsed.SKU),
		ParsedDistributorID: parsedDistributorID,
		ParsedMpn:           strPtr(parsed.MPN),
		ParsedManufacturer:  strPtr(parsed.Manufacturer),
		Metadata:            map[string]any{"raw_barcode": body.Barcode},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                     "not_found_in_db",
		"parsed_mpn":                 parsed.MPN,
		"parsed_manufacturer":        parsed.Manufacturer,
		"parsed_sku":                 parsed.SKU,
		"parsed_distributor":         parsed.Distributor,
		"parsed_distributor_id":      parsedDistributorID,
		"preferred_distributor_id":   preferredDistributorID,
		"preferred_distributor_name": preferredDistributorName,
		"parsed_qty":                 parsed.Quantity,
		"parsed_lot":                 parsed.Lot,
		"can_auto_import":            canAutoImport,
		"distributor_configs":        distConfigs,
	})
}

// writeLookupFound writes the "found" response for LookupScanBarcode,
// auto-creating the team's inventory entry at quantity 0 if one does not exist.
func (h *Handler) writeLookupFound(w http.ResponseWriter, r *http.Request,
	sess *dbgen.ScanSession,
	teamID pgtype.UUID, partID pgtype.UUID, mpn, manufacturer, desc, shortDesc *string,
	sku, distributorName *string, invEntryID pgtype.UUID, qoh *int32,
	storageLocationID pgtype.UUID, storageCoordinate []int32,
	parsed *scanner.ParsedBarcode) {

	newlyCreated := false
	finalEntryID := invEntryID
	finalQoh := qoh
	finalStorageLocationID := storageLocationID
	finalStorageCoordinate := storageCoordinate

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
		finalStorageLocationID = pgtype.UUID{}
		finalStorageCoordinate = nil
		newlyCreated = true
	}

	d := desc
	if d == nil {
		d = shortDesc
	}

	var storageLocation any
	if finalStorageLocationID.Valid {
		storageLocation = map[string]any{
			"id":         finalStorageLocationID.String(),
			"coordinate": finalStorageCoordinate,
		}
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
		"storage_location":              storageLocation,
		"inventory_entry_newly_created": newlyCreated,
		"bag_qty":                       parsed.Quantity,
	}
	if sku != nil {
		resp["sku"] = *sku
	}
	if distributorName != nil {
		resp["distributor_name"] = *distributorName
	}

	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "bag_scanned",
		EntityType: strPtr("part"),
		EntityID:   partID,
		EntityName: mpn,
		ParsedSku:  sku,
		Metadata:   map[string]any{"newly_created": newlyCreated},
	})

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
	parsed, parseErr := scanner.ParseAny(body.Barcode)
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

	// Role enforcement: member+ required to confirm stock receipt.
	membership, err := h.checkTeamMembership(r.Context(), sess.TeamID, sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if membership == nil || !roleAtLeast(membership.Role, "member") {
		writeError(w, http.StatusForbidden, "member role required", "FORBIDDEN")
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

	// Audit log for order_received.
	qtyAudit := qty
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "order_received",
		EntityType: strPtr("part"),
		EntityID:   li.PartID,
		EntityName: strPtr(ps.MPN),
		Quantity:   &qtyAudit,
		Metadata:   map[string]any{"order_id": orderID.String(), "line_item_id": lineItemID.String()},
	})

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

	var storageLoc any
	if invEntry.StorageLocationID.Valid {
		storageLoc = map[string]any{
			"id":         invEntry.StorageLocationID.String(),
			"coordinate": invEntry.StorageCoordinate,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                    true,
		"new_quantity_received": updatedLI.QuantityReceived,
		"new_quantity_on_hand":  newQty,
		"order_status":          string(newOrderStatus),
		"inventory_entry_id":    invEntry.ID.String(),
		"storage_location":      storageLoc,
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

	var quantityReserved int32
	if part.InventoryEntryID.Valid {
		quantityReserved, err = h.Queries.SumReservedForPart(ctx, dbgen.SumReservedForPartParams{
			PartID: partID,
			TeamID: sess.TeamID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
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
		"inventory_entry_id": invEntryID,
		"quantity_on_hand":   part.QuantityOnHand,
		"quantity_reserved":  quantityReserved,
		"quantity_available": max(int32(0), func() int32 {
			if part.QuantityOnHand != nil {
				return *part.QuantityOnHand
			}
			return 0
		}() - quantityReserved),
		"storage_location": func() any {
			if part.StorageLocationID.Valid {
				return map[string]any{"id": part.StorageLocationID.String(), "coordinate": part.StorageCoordinate}
			}
			return nil
		}(),
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

	// Role enforcement: member+ required to import parts.
	membership, err := h.checkTeamMembership(r.Context(), sess.TeamID, sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if membership == nil || !roleAtLeast(membership.Role, "member") {
		writeError(w, http.StatusForbidden, "member role required", "FORBIDDEN")
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

	// Audit log for part_imported.
	part, partErr := h.Queries.GetPart(r.Context(), partID)
	var partMPN *string
	if partErr == nil {
		partMPN = part.Mpn
	}
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "part_imported",
		EntityType: strPtr("part"),
		EntityID:   partID,
		EntityName: partMPN,
		ParsedSku:  &req.SKU,
	})
	// Resolve any part_not_found audit rows that match this SKU.
	distID := req.DistributorID
	_ = h.Queries.ResolvePartNotFoundBySKU(r.Context(), dbgen.ResolvePartNotFoundBySKUParams{
		PartID:              partID,
		PartName:            partMPN,
		ParsedSku:           &req.SKU,
		ParsedDistributorID: &distID,
	})
	if partErr == nil && part.Mpn != nil && part.Manufacturer != nil {
		_ = h.Queries.ResolvePartNotFoundByMPN(r.Context(), dbgen.ResolvePartNotFoundByMPNParams{
			PartID:             partID,
			PartName:           part.Mpn,
			ParsedMpn:          part.Mpn,
			ParsedManufacturer: part.Manufacturer,
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"part_id": partID.String()})
}

// ImportScanPartMulti handles POST /api/v1/scan/parts/import-multi
// Body: {"listings":[{"distributor_id":"uuid","sku":"SKU"}]} (≥1 required).
// Returns 200 {"part_id":"uuid","failed":[...]} where failed is empty on full success.
// All listings with the same MPN+manufacturer converge to the same part via deduplication.
func (h *Handler) ImportScanPartMulti(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	membership, err := h.checkTeamMembership(r.Context(), sess.TeamID, sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if membership == nil || !roleAtLeast(membership.Role, "member") {
		writeError(w, http.StatusForbidden, "member role required", "FORBIDDEN")
		return
	}

	var req struct {
		Listings []struct {
			DistributorID string `json:"distributor_id"`
			SKU           string `json:"sku"`
		} `json:"listings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Listings) == 0 {
		writeError(w, http.StatusBadRequest, "listings array with at least one entry is required", "BAD_REQUEST")
		return
	}
	for _, l := range req.Listings {
		if l.DistributorID == "" || l.SKU == "" {
			writeError(w, http.StatusBadRequest, "each listing must have distributor_id and sku", "BAD_REQUEST")
			return
		}
	}

	type failedListing struct {
		DistributorID string `json:"distributor_id"`
		SKU           string `json:"sku"`
		Reason        string `json:"reason"`
	}

	var lastPartID pgtype.UUID
	var anySuccess bool
	failed := []failedListing{}

	for _, listing := range req.Listings {
		pid, err := h.importPartBySKU(r.Context(), sess.TeamID, listing.DistributorID, listing.SKU)
		if err != nil {
			if errors.Is(err, errPartNotFoundAtDistributor) {
				failed = append(failed, failedListing{listing.DistributorID, listing.SKU, "not_found"})
				continue
			}
			h.writeImportError(w, "ImportScanPartMulti", listing.SKU, err)
			return
		}
		lastPartID = pid
		anySuccess = true

		// Audit log per successfully imported part.
		listingCopy := listing
		importedPart, partErr := h.Queries.GetPart(r.Context(), pid)
		var importedMPN *string
		if partErr == nil {
			importedMPN = importedPart.Mpn
		}
		distID := listingCopy.DistributorID
		h.writeAuditLog(r.Context(), sess, auditLogEntry{
			EventType:  "part_imported",
			EntityType: strPtr("part"),
			EntityID:   pid,
			EntityName: importedMPN,
			ParsedSku:  &listingCopy.SKU,
		})
		_ = h.Queries.ResolvePartNotFoundBySKU(r.Context(), dbgen.ResolvePartNotFoundBySKUParams{
			PartID:              pid,
			PartName:            importedMPN,
			ParsedSku:           &listingCopy.SKU,
			ParsedDistributorID: &distID,
		})
		if partErr == nil && importedPart.Mpn != nil && importedPart.Manufacturer != nil {
			_ = h.Queries.ResolvePartNotFoundByMPN(r.Context(), dbgen.ResolvePartNotFoundByMPNParams{
				PartID:             pid,
				PartName:           importedPart.Mpn,
				ParsedMpn:          importedPart.Mpn,
				ParsedManufacturer: importedPart.Manufacturer,
			})
		}
	}

	if !anySuccess {
		writeError(w, http.StatusNotFound, "no listings could be imported", "PART_NOT_FOUND")
		return
	}

	h.publishEvent(r, "part_changed", sess.TeamID.String(), map[string]string{"part_id": lastPartID.String()})
	writeJSON(w, http.StatusOK, map[string]any{
		"part_id": lastPartID.String(),
		"failed":  failed,
	})
}

// ListScanStorageLocations handles GET /api/v1/scan/storage-locations.
// Returns the full storage location tree for the team so the mobile picker can
// build paths and detect grid-enabled locations client-side.
func (h *Handler) ListScanStorageLocations(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	locations, err := h.Queries.ListStorageLocations(r.Context(), sess.TeamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type locationItem struct {
		ID          string          `json:"id"`
		ParentID    *string         `json:"parent_id"`
		Label       string          `json:"label"`
		Description *string         `json:"description"`
		GridConfig  json.RawMessage `json:"grid_config"`
	}
	items := make([]locationItem, len(locations))
	for i, loc := range locations {
		item := locationItem{
			ID:          loc.ID.String(),
			Label:       loc.Label,
			Description: loc.Description,
			GridConfig:  loc.GridConfig,
		}
		if loc.ParentID.Valid {
			s := loc.ParentID.String()
			item.ParentID = &s
		}
		items[i] = item
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": items})
}

// ListScanStorageInventory handles GET /api/v1/scan/storage/{locationId}/inventory.
// Returns all inventory entries in the given location and its descendants (up to 500).
func (h *Handler) ListScanStorageInventory(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	locationIDStr := chi.URLParam(r, "locationId")
	var locationID pgtype.UUID
	if err := locationID.Scan(locationIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location id", "BAD_REQUEST")
		return
	}

	entries, err := h.Queries.ListInventoryByLocation(r.Context(), dbgen.ListInventoryByLocationParams{
		LocationID: locationID,
		TeamID:     sess.TeamID,
		Limit:      500,
		Offset:     0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type storageLocationRef struct {
		ID         string  `json:"id"`
		Coordinate []int32 `json:"coordinate"`
	}
	type entryItem struct {
		InventoryEntryID string              `json:"inventory_entry_id"`
		PartID           string              `json:"part_id"`
		MPN              *string             `json:"mpn"`
		Description      *string             `json:"description"`
		QuantityOnHand   int32               `json:"quantity_on_hand"`
		StorageLocation  *storageLocationRef `json:"storage_location"`
	}
	items := make([]entryItem, len(entries))
	for i, e := range entries {
		item := entryItem{
			InventoryEntryID: e.ID.String(),
			PartID:           e.PartID.String(),
			MPN:              e.PartMpn,
			Description:      e.PartDescription,
			QuantityOnHand:   e.QuantityOnHand,
		}
		if e.StorageLocationID.Valid {
			item.StorageLocation = &storageLocationRef{
				ID:         e.StorageLocationID.String(),
				Coordinate: e.StorageCoordinate,
			}
		}
		items[i] = item
	}
	writeJSON(w, http.StatusOK, items)
}

// PatchScanInventoryEntry handles PATCH /api/v1/scan/inventory-entries/{id}.
// Updates the storage_location (id + coordinate) of an inventory entry.
// Body: { "storage_location": { "id": "uuid", "coordinate": [1,2] } | null }
func (h *Handler) PatchScanInventoryEntry(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	membership, err := h.checkTeamMembership(r.Context(), sess.TeamID, sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if membership == nil || !roleAtLeast(membership.Role, "member") {
		writeError(w, http.StatusForbidden, "member role required", "FORBIDDEN")
		return
	}

	entryIDStr := chi.URLParam(r, "id")
	var entryID pgtype.UUID
	if err := entryID.Scan(entryIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid inventory entry id", "BAD_REQUEST")
		return
	}

	var body struct {
		StorageLocation *struct {
			ID         string  `json:"id"`
			Coordinate []int32 `json:"coordinate"`
		} `json:"storage_location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	params := dbgen.UpdateInventoryEntryParams{
		ID:                   entryID,
		TeamID:               sess.TeamID,
		SetStorageLocationID: true,
		SetStorageCoordinate: true,
	}
	if body.StorageLocation != nil {
		var locID pgtype.UUID
		if err := locID.Scan(body.StorageLocation.ID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid storage_location.id", "BAD_REQUEST")
			return
		}
		params.StorageLocationID = locID
		params.StorageCoordinate = body.StorageLocation.Coordinate
	}

	entry, err := h.Queries.UpdateInventoryEntry(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "inventory entry not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "inventory_changed", sess.TeamID.String(), map[string]string{
		"entry_id": entry.ID.String(),
		"part_id":  entry.PartID.String(),
	})

	// Audit log for storage_assigned.
	meta := map[string]any{"inventory_entry_id": entry.ID.String()}
	if entry.StorageLocationID.Valid {
		meta["location_id"] = entry.StorageLocationID.String()
	}
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "storage_assigned",
		EntityType: strPtr("part"),
		EntityID:   entry.PartID,
		Metadata:   meta,
	})

	var storageLocation any
	if entry.StorageLocationID.Valid {
		storageLocation = map[string]any{
			"id":         entry.StorageLocationID.String(),
			"coordinate": entry.StorageCoordinate,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               entry.ID.String(),
		"storage_location": storageLocation,
	})
}

// ── Audit log ─────────────────────────────────────────────────────────────────

// auditLogEntry holds the fields for a single scan audit log write.
type auditLogEntry struct {
	EventType           string
	EntityType          *string
	EntityID            pgtype.UUID
	EntityName          *string
	Quantity            *int32
	ParsedSku           *string
	ParsedDistributorID *string
	ParsedMpn           *string
	ParsedManufacturer  *string
	Metadata            map[string]any
}

// writeAuditLog persists a scan_audit_log row and publishes scan_audit_changed
// to the desktop. Errors are silently dropped — audit failures must not affect
// the primary operation.
func (h *Handler) writeAuditLog(ctx context.Context, sess *dbgen.ScanSession, e auditLogEntry) {
	var metadata []byte
	if len(e.Metadata) > 0 {
		metadata, _ = json.Marshal(e.Metadata)
	}
	_, _ = h.Queries.CreateScanAuditLog(ctx, dbgen.CreateScanAuditLogParams{
		TeamID:              sess.TeamID,
		UserID:              sess.UserID,
		SessionID:           sess.ID,
		EventType:           e.EventType,
		EntityType:          e.EntityType,
		EntityID:            e.EntityID,
		EntityName:          e.EntityName,
		Quantity:            e.Quantity,
		ParsedSku:           e.ParsedSku,
		ParsedDistributorID: e.ParsedDistributorID,
		ParsedMpn:           e.ParsedMpn,
		ParsedManufacturer:  e.ParsedManufacturer,
		Metadata:            metadata,
	})
	d, _ := json.Marshal(map[string]string{"event_type": e.EventType})
	h.Worker.Publish(worker.Event{
		Type:         "scan_audit_changed",
		TeamID:       sess.TeamID.String(),
		TargetUserID: sess.UserID.String(),
		Data:         d,
	})
}

// strPtr returns a pointer to s if s is non-empty, else nil.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ── Desktop endpoint: scan audit log ─────────────────────────────────────────

// ListScanAuditLog handles GET /api/v1/teams/{teamId}/scan-audit-log.
// Returns the authenticated user's own scan activity with cursor-based pagination.
func (h *Handler) ListScanAuditLog(w http.ResponseWriter, r *http.Request) {
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

	// Parse query params.
	var eventTypes []string
	if raw := r.URL.Query().Get("event_type"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(s); t != "" {
				eventTypes = append(eventTypes, t)
			}
		}
	}

	limit := int32(50)
	if s := r.URL.Query().Get("limit"); s != "" {
		var v int32
		if _, err := fmt.Sscanf(s, "%d", &v); err == nil && v > 0 {
			limit = min(v, 100)
		}
	}

	var cursorTime pgtype.Timestamptz
	var cursorID pgtype.UUID
	if ct := r.URL.Query().Get("cursor_time"); ct != "" {
		t, err := time.Parse(time.RFC3339Nano, ct)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor_time", "BAD_REQUEST")
			return
		}
		cursorTime.Scan(t) //nolint:errcheck
		if ci := r.URL.Query().Get("cursor_id"); ci != "" {
			cursorID.Scan(ci) //nolint:errcheck
		}
	}

	rows, err := h.Queries.ListScanAuditLog(r.Context(), dbgen.ListScanAuditLogParams{
		TeamID:     teamID,
		UserID:     userID,
		EventTypes: eventTypes,
		CursorTime: cursorTime,
		CursorID:   cursorID,
		Limit:      limit + 1, // fetch one extra to determine if there's a next page
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type auditItem struct {
		ID                       string          `json:"id"`
		EventType                string          `json:"event_type"`
		EntityType               *string         `json:"entity_type"`
		EntityID                 *string         `json:"entity_id"`
		EntityName               *string         `json:"entity_name"`
		Quantity                 *int32          `json:"quantity"`
		ParsedSku                *string         `json:"parsed_sku"`
		ParsedDistributorID      *string         `json:"parsed_distributor_id"`
		ParsedMpn                *string         `json:"parsed_mpn"`
		ParsedManufacturer       *string         `json:"parsed_manufacturer"`
		Metadata                 json.RawMessage `json:"metadata"`
		CreatedAt                string          `json:"created_at"`
		PartManufacturer         *string         `json:"part_manufacturer"`
		PartShortDescription     *string         `json:"part_short_description"`
		PartCategoryName         *string         `json:"part_category_name"`
		InventoryEntryID         *string         `json:"inventory_entry_id"`
		InventoryQuantityOnHand  *int32          `json:"inventory_quantity_on_hand"`
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]auditItem, len(rows))
	for i, r := range rows {
		item := auditItem{
			ID:                  r.ID.String(),
			EventType:           r.EventType,
			EntityType:          r.EntityType,
			EntityName:          r.EntityName,
			Quantity:            r.Quantity,
			ParsedSku:           r.ParsedSku,
			ParsedDistributorID: r.ParsedDistributorID,
			ParsedMpn:           r.ParsedMpn,
			ParsedManufacturer:  r.ParsedManufacturer,
			Metadata:            r.Metadata,
			CreatedAt:           r.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
			PartManufacturer:    r.PartManufacturer,
			PartShortDescription: r.PartShortDescription,
			PartCategoryName:    r.PartCategoryName,
		}
		if r.EntityID.Valid {
			s := r.EntityID.String()
			item.EntityID = &s
		}
		if r.InventoryEntryID.Valid {
			s := r.InventoryEntryID.String()
			item.InventoryEntryID = &s
			item.InventoryQuantityOnHand = r.InventoryQuantityOnHand
		}
		items[i] = item
	}

	var nextCursorTime, nextCursorID *string
	if hasMore && len(items) > 0 {
		last := rows[len(rows)-1]
		t := last.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		id := last.ID.String()
		nextCursorTime = &t
		nextCursorID = &id
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":            items,
		"next_cursor_time": nextCursorTime,
		"next_cursor_id":   nextCursorID,
	})
}

// ── Scan-to-Focus ─────────────────────────────────────────────────────────────

// ScanFocus handles POST /api/v1/scan/focus.
// Phone sends an entity type + ID; the backend publishes a focus event to the
// desktop SSE channel. Returns 409 if the desktop is not currently connected.
func (h *Handler) ScanFocus(w http.ResponseWriter, r *http.Request) {
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
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EntityType == "" || body.EntityID == "" {
		writeError(w, http.StatusBadRequest, "entity_type and entity_id are required", "BAD_REQUEST")
		return
	}

	validTypes := map[string]bool{"part": true, "order": true, "storage": true, "build": true}
	if !validTypes[body.EntityType] {
		writeError(w, http.StatusBadRequest, "entity_type must be part, order, storage, or build", "BAD_REQUEST")
		return
	}

	if !h.Worker.HasSubscriber(sess.TeamID.String(), sess.UserID.String()) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "desktop is not connected",
			"code":  "DESKTOP_NOT_CONNECTED",
		})
		return
	}

	d, _ := json.Marshal(map[string]string{
		"entity_type": body.EntityType,
		"entity_id":   body.EntityID,
	})
	h.Worker.Publish(worker.Event{
		Type:         "scan_focus_requested",
		TeamID:       sess.TeamID.String(),
		TargetUserID: sess.UserID.String(),
		Data:         d,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ── Build Kitting ─────────────────────────────────────────────────────────────

// StartKitting handles POST /api/v1/teams/{teamId}/scan-sessions/{sessionId}/kitting.
// Desktop initiates a kitting session for a build; pushes context to the phone.
func (h *Handler) StartKitting(w http.ResponseWriter, r *http.Request) {
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
		BuildID string `json:"build_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BuildID == "" {
		writeError(w, http.StatusBadRequest, "build_id is required", "BAD_REQUEST")
		return
	}
	var buildID pgtype.UUID
	if err := buildID.Scan(body.BuildID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid build_id", "BAD_REQUEST")
		return
	}

	// Look up the scan session and verify it belongs to this team + user.
	sess, err := h.Queries.GetScanSession(r.Context(), dbgen.GetScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "scan session not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess.UserID != userID {
		writeError(w, http.StatusForbidden, "can only start kitting on your own scan session", "FORBIDDEN")
		return
	}

	// Look up build and validate status.
	build, err := h.Queries.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
		ID:     buildID,
		TeamID: teamID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	validStatuses := map[string]bool{"pending": true, "ordered": true, "ready": true}
	if !validStatuses[string(build.Status)] {
		writeError(w, http.StatusConflict, "build must be in pending, ordered, or ready status", "INVALID_BUILD_STATUS")
		return
	}

	// Check for an existing in-progress kitting session for this build.
	existing, err := h.Queries.GetActiveKittingSessionForBuild(r.Context(), buildID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err == nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":              "kitting already in progress for this build",
			"code":               "KITTING_ALREADY_IN_PROGRESS",
			"kitting_session_id": existing.ID.String(),
		})
		return
	}

	// Create kitting session and seed confirmations.
	ks, err := h.Queries.CreateKittingSession(r.Context(), dbgen.CreateKittingSessionParams{
		ScanSessionID: sessionID,
		BuildID:       buildID,
		TeamID:        teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := h.Queries.SeedKittingConfirmations(r.Context(), dbgen.SeedKittingConfirmationsParams{
		KittingSessionID: ks.ID,
		BuildID:          buildID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Push to phone.
	buildRef := fmt.Sprintf("%s × %d", "Build", build.Quantity)
	msg, _ := json.Marshal(map[string]any{
		"type":                        "scan_context_changed",
		"context_type":                "kitting",
		"context_kitting_session_id":  ks.ID.String(),
		"build_ref":                   buildRef,
	})
	h.ScanHub.Send(sessionID.String(), msg)

	writeJSON(w, http.StatusCreated, map[string]any{
		"kitting_session_id": ks.ID.String(),
	})
}

// kittingSessionFromToken fetches the kitting session and validates that the
// current scan token's session owns it. Returns nil if validation fails.
func (h *Handler) kittingSessionFromToken(r *http.Request, kittingSessionID pgtype.UUID) (*dbgen.ScanSession, *dbgen.GetKittingSessionRow, error) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		return nil, nil, err
	}
	if sess == nil {
		return nil, nil, nil //nolint:nilnil
	}
	ks, err := h.Queries.GetKittingSession(r.Context(), kittingSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, nil, nil //nolint:nilnil
	}
	if err != nil {
		return nil, nil, err
	}
	if ks.ScanSessionID != sess.ID {
		return sess, nil, nil //nolint:nilnil
	}
	return sess, &ks, nil
}

// kittingProgress fetches progress counts and returns them as a map.
func (h *Handler) kittingProgress(ctx context.Context, kittingSessionID pgtype.UUID) (map[string]int32, error) {
	rows, err := h.Queries.GetKittingProgress(ctx, kittingSessionID)
	if err != nil {
		return nil, err
	}
	m := map[string]int32{"pending": 0, "confirmed": 0, "shortfall": 0, "skipped": 0}
	for _, row := range rows {
		m[row.Status] = row.Count
	}
	return m, nil
}

// publishKittingChanged sends a build_kitting_changed SSE event to the desktop.
func (h *Handler) publishKittingChanged(sess *dbgen.ScanSession, ks *dbgen.GetKittingSessionRow) {
	d, _ := json.Marshal(map[string]string{
		"build_id":           ks.BuildID.String(),
		"kitting_session_id": ks.ID.String(),
	})
	h.Worker.Publish(worker.Event{
		Type:         "build_kitting_changed",
		TeamID:       sess.TeamID.String(),
		TargetUserID: sess.UserID.String(),
		Data:         d,
	})
}

// GetKittingSessionHandler handles GET /api/v1/scan/kitting/{kittingSessionId}.
func (h *Handler) GetKittingSessionHandler(w http.ResponseWriter, r *http.Request) {
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	sess, ks, err := h.kittingSessionFromToken(r, ksID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if ks == nil {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	progress, err := h.kittingProgress(r.Context(), ks.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	buildRef := fmt.Sprintf("%s %s × %d", ks.ProjectName, ks.BomRevisionLabel, ks.BuildQuantity)
	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{
			"id":        ks.ID.String(),
			"build_id":  ks.BuildID.String(),
			"build_ref": buildRef,
			"status":    ks.Status,
		},
		"progress": progress,
	})
}

// GetKittingItemsHandler handles GET /api/v1/scan/kitting/{kittingSessionId}/items.
func (h *Handler) GetKittingItemsHandler(w http.ResponseWriter, r *http.Request) {
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	sess, ks, err := h.kittingSessionFromToken(r, ksID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if ks == nil {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	items, err := h.Queries.GetKittingItems(r.Context(), dbgen.GetKittingItemsParams{
		TeamID:           sess.TeamID,
		KittingSessionID: ks.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type kittingItemResp struct {
		PartID              string  `json:"part_id"`
		PartMPN             *string `json:"part_mpn"`
		PartManufacturer    *string `json:"part_manufacturer"`
		PartDescription     *string `json:"part_description"`
		QuantityNeeded      int32   `json:"quantity_needed"`
		QuantityConfirmed   int32   `json:"quantity_confirmed"`
		Status              string  `json:"status"`
		StorageLocationID   *string `json:"storage_location_id"`
		StorageLocationPath *string `json:"storage_location_path"`
		StorageCoordinate   []int32 `json:"storage_coordinate"`
	}

	resp := make([]kittingItemResp, len(items))
	for i, item := range items {
		ki := kittingItemResp{
			PartID:            item.PartID.String(),
			PartMPN:           item.PartMpn,
			PartManufacturer:  item.PartManufacturer,
			PartDescription:   item.PartDescription,
			QuantityNeeded:    item.QuantityNeeded,
			QuantityConfirmed: item.QuantityConfirmed,
			Status:            item.Status,
			StorageCoordinate: item.StorageCoordinate,
		}
		if item.StorageLocationID.Valid {
			s := item.StorageLocationID.String()
			ki.StorageLocationID = &s
			path := item.StorageLocationPath
			ki.StorageLocationPath = &path
		}
		resp[i] = ki
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": resp})
}

// KittingConfirm handles POST /api/v1/scan/kitting/{kittingSessionId}/confirm.
func (h *Handler) KittingConfirm(w http.ResponseWriter, r *http.Request) {
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	sess, ks, err := h.kittingSessionFromToken(r, ksID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if ks == nil {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	var body struct {
		PartID            string `json:"part_id"`
		QuantityConfirmed int32  `json:"quantity_confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PartID == "" {
		writeError(w, http.StatusBadRequest, "part_id is required", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(body.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}

	item, err := h.Queries.UpsertKittingConfirmation(r.Context(), dbgen.UpsertKittingConfirmationParams{
		KittingSessionID:  ks.ID,
		PartID:            partID,
		Status:            "confirmed",
		QuantityConfirmed: body.QuantityConfirmed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "kitting item not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	qty := item.QuantityConfirmed
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "kitting_confirmed",
		EntityType: strPtr("part"),
		EntityID:   partID,
		Quantity:   &qty,
		Metadata:   map[string]any{"kitting_session_id": ks.ID.String(), "build_id": ks.BuildID.String()},
	})
	h.publishKittingChanged(sess, ks)

	progress, err := h.kittingProgress(r.Context(), ks.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"item": map[string]any{
			"part_id":            item.PartID.String(),
			"quantity_needed":    item.QuantityNeeded,
			"quantity_confirmed": item.QuantityConfirmed,
			"status":             item.Status,
		},
		"progress": progress,
	})
}

// KittingShortfall handles POST /api/v1/scan/kitting/{kittingSessionId}/shortfall.
func (h *Handler) KittingShortfall(w http.ResponseWriter, r *http.Request) {
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	sess, ks, err := h.kittingSessionFromToken(r, ksID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if ks == nil {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	var body struct {
		PartID            string `json:"part_id"`
		QuantityAvailable int32  `json:"quantity_available"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PartID == "" {
		writeError(w, http.StatusBadRequest, "part_id is required", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(body.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}

	item, err := h.Queries.UpsertKittingConfirmation(r.Context(), dbgen.UpsertKittingConfirmationParams{
		KittingSessionID:  ks.ID,
		PartID:            partID,
		Status:            "shortfall",
		QuantityConfirmed: body.QuantityAvailable,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "kitting item not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	qty := item.QuantityConfirmed
	h.writeAuditLog(r.Context(), sess, auditLogEntry{
		EventType:  "kitting_shortfall",
		EntityType: strPtr("part"),
		EntityID:   partID,
		Quantity:   &qty,
		Metadata:   map[string]any{"kitting_session_id": ks.ID.String(), "build_id": ks.BuildID.String(), "quantity_needed": item.QuantityNeeded},
	})
	h.publishKittingChanged(sess, ks)

	progress, err := h.kittingProgress(r.Context(), ks.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"item": map[string]any{
			"part_id":            item.PartID.String(),
			"quantity_needed":    item.QuantityNeeded,
			"quantity_confirmed": item.QuantityConfirmed,
			"status":             item.Status,
		},
		"progress": progress,
	})
}

// KittingComplete handles POST /api/v1/scan/kitting/{kittingSessionId}/complete.
func (h *Handler) KittingComplete(w http.ResponseWriter, r *http.Request) {
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	sess, ks, err := h.kittingSessionFromToken(r, ksID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if ks == nil {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	if err := h.Queries.CompleteKittingSession(r.Context(), ks.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishKittingChanged(sess, ks)

	progress, err := h.kittingProgress(r.Context(), ks.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"progress": progress})
}

// KittingAbandon handles POST /api/v1/scan/kitting/{kittingSessionId}/abandon.
func (h *Handler) KittingAbandon(w http.ResponseWriter, r *http.Request) {
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	sess, ks, err := h.kittingSessionFromToken(r, ksID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if ks == nil {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	if err := h.Queries.AbandonKittingSession(r.Context(), ks.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishKittingChanged(sess, ks)
	w.WriteHeader(http.StatusNoContent)
}

// ── Desktop-facing kitting read endpoints ─────────────────────────────────────

// GetKittingSessionDesktop handles GET /api/v1/teams/{teamId}/kitting/{kittingSessionId}.
func (h *Handler) GetKittingSessionDesktop(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	ks, err := h.Queries.GetKittingSession(r.Context(), ksID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if ks.TeamID != teamID {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	progress, err := h.kittingProgress(r.Context(), ks.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	buildRef := fmt.Sprintf("%s %s × %d", ks.ProjectName, ks.BomRevisionLabel, ks.BuildQuantity)
	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{
			"id":        ks.ID.String(),
			"build_id":  ks.BuildID.String(),
			"build_ref": buildRef,
			"status":    ks.Status,
		},
		"progress": progress,
	})
}

// GetKittingItemsDesktop handles GET /api/v1/teams/{teamId}/kitting/{kittingSessionId}/items.
func (h *Handler) GetKittingItemsDesktop(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var ksID pgtype.UUID
	if err := ksID.Scan(chi.URLParam(r, "kittingSessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kitting session id", "BAD_REQUEST")
		return
	}

	ks, err := h.Queries.GetKittingSession(r.Context(), ksID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if ks.TeamID != teamID {
		writeError(w, http.StatusNotFound, "kitting session not found", "NOT_FOUND")
		return
	}

	items, err := h.Queries.GetKittingItems(r.Context(), dbgen.GetKittingItemsParams{
		TeamID:           teamID,
		KittingSessionID: ks.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type kittingItemResp struct {
		PartID              string  `json:"part_id"`
		PartMPN             *string `json:"part_mpn"`
		PartManufacturer    *string `json:"part_manufacturer"`
		PartDescription     *string `json:"part_description"`
		QuantityNeeded      int32   `json:"quantity_needed"`
		QuantityConfirmed   int32   `json:"quantity_confirmed"`
		Status              string  `json:"status"`
		StorageLocationID   *string `json:"storage_location_id"`
		StorageLocationPath *string `json:"storage_location_path"`
		StorageCoordinate   []int32 `json:"storage_coordinate"`
	}

	resp := make([]kittingItemResp, len(items))
	for i, item := range items {
		ki := kittingItemResp{
			PartID:            item.PartID.String(),
			PartMPN:           item.PartMpn,
			PartManufacturer:  item.PartManufacturer,
			PartDescription:   item.PartDescription,
			QuantityNeeded:    item.QuantityNeeded,
			QuantityConfirmed: item.QuantityConfirmed,
			Status:            item.Status,
			StorageCoordinate: item.StorageCoordinate,
		}
		if item.StorageLocationID.Valid {
			s := item.StorageLocationID.String()
			ki.StorageLocationID = &s
			path := item.StorageLocationPath
			ki.StorageLocationPath = &path
		}
		resp[i] = ki
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": resp})
}

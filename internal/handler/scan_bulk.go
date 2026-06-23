package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
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

const bulkSessionInactivityTimeout = 15 * time.Minute

// bulkSessionState returns "active" or "timed_out" based on last_item_at.
func bulkSessionState(s dbgen.BulkScanSession) string {
	if !s.LastItemAt.Valid {
		return "timed_out"
	}
	if time.Since(s.LastItemAt.Time) > bulkSessionInactivityTimeout {
		return "timed_out"
	}
	return "active"
}

// requireBulkSessionOwned verifies the bulk scan session named by sessionID
// belongs to the scan token's team and user. Bulk sessions are per-user scanning
// workspaces, so a scan token may only touch its own. Writes 404 and returns
// false otherwise.
func (h *Handler) requireBulkSessionOwned(w http.ResponseWriter, r *http.Request, sessionID pgtype.UUID, sess *dbgen.ScanSession) bool {
	bs, err := h.Queries.GetBulkScanSession(r.Context(), dbgen.GetBulkScanSessionParams{
		ID:     sessionID,
		TeamID: sess.TeamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "bulk scan session not found", "NOT_FOUND")
			return false
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return false
	}
	if bs.UserID != sess.UserID {
		writeError(w, http.StatusNotFound, "bulk scan session not found", "NOT_FOUND")
		return false
	}
	return true
}

// ── Phone handlers (scan token auth) ─────────────────────────────────────────

// StartBulkSession handles POST /api/v1/scan/bulk/start.
func (h *Handler) StartBulkSession(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	bs, err := h.Queries.InsertBulkScanSession(r.Context(), dbgen.InsertBulkScanSessionParams{
		UserID: sess.UserID,
		TeamID: sess.TeamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": bs.ID.String(),
		"item_count": 0,
	})
}

// AddBulkScanItem handles POST /api/v1/scan/bulk/sessions/{sessionId}/items.
// Performs a synchronous DB lookup. For known parts it sets task_type immediately.
// For unknown parts it creates a part_resolve_item and enqueues enrichment.
func (h *Handler) AddBulkScanItem(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	var sessionID pgtype.UUID
	if err := sessionID.Scan(chi.URLParam(r, "sessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id", "BAD_REQUEST")
		return
	}
	if !h.requireBulkSessionOwned(w, r, sessionID, sess) {
		return
	}

	var body struct {
		Barcode string `json:"barcode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Barcode == "" {
		writeError(w, http.StatusBadRequest, "barcode required", "BAD_REQUEST")
		return
	}

	ctx := r.Context()

	// Parse the barcode to extract part identifiers.
	parsed, parseErr := scanner.ParseAny(body.Barcode)
	if parseErr != nil {
		slog.Debug("bulk scan: barcode unparseable", "session_id", sessionID.String(), "barcode", body.Barcode, "err", parseErr)
	} else if parsed != nil {
		slog.Debug("bulk scan: barcode parsed", "session_id", sessionID.String(), "distributor", parsed.Distributor, "sku", parsed.SKU, "mpn", parsed.MPN, "manufacturer", parsed.Manufacturer)
	}

	// Resolve distributor short-name → canonical DB name + UUID.
	var resolvedDistributorName string
	var resolvedDistributorID pgtype.UUID
	if parseErr == nil && parsed != nil && parsed.Distributor != "" {
		if cfgs, err := h.Queries.ListEnabledDistributorConfigsForTeam(ctx, sess.TeamID); err == nil {
			lowerParsed := strings.ToLower(parsed.Distributor)
			for _, cfg := range cfgs {
				if strings.Contains(strings.ToLower(cfg.DistributorName), lowerParsed) {
					resolvedDistributorName = cfg.DistributorName
					resolvedDistributorID = cfg.DistributorID
					break
				}
			}
		}
	}

	// Build parsed_fields JSON.
	var parsedFields []byte
	var encodedQty *int32
	var sku, mpn, manufacturer string
	if parseErr == nil && parsed != nil {
		sku = parsed.SKU
		mpn = parsed.MPN
		manufacturer = parsed.Manufacturer
		distName := resolvedDistributorName
		if distName == "" {
			distName = parsed.Distributor
		}
		pf := map[string]any{
			"distributor":    distName,
			"distributor_id": resolvedDistributorID.String(),
			"sku":            sku,
			"mpn":            mpn,
			"manufacturer":   manufacturer,
			"encoded_qty":    parsed.Quantity,
		}
		parsedFields, _ = json.Marshal(pf)
		if parsed.Quantity > 0 {
			q := int32(parsed.Quantity)
			encodedQty = &q
		}
	}

	// ── Phase 1: synchronous DB lookup ───────────────────────────────────────

	var foundPartID pgtype.UUID
	var hasInventory bool

	if sku != "" && resolvedDistributorID.Valid {
		row, err := h.Queries.GetPartBySKUAndDistributorIDWithInventory(ctx, dbgen.GetPartBySKUAndDistributorIDWithInventoryParams{
			Sku: sku, TeamID: sess.TeamID, DistributorID: resolvedDistributorID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
		}
	}
	if !foundPartID.Valid && sku != "" {
		row, err := h.Queries.GetPartBySKUWithInventory(ctx, dbgen.GetPartBySKUWithInventoryParams{
			Sku: sku, TeamID: sess.TeamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
		}
	}
	if !foundPartID.Valid && mpn != "" && manufacturer != "" {
		row, err := h.Queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
			Mpn: &mpn, Manufacturer: &manufacturer, TeamID: sess.TeamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
		}
	}
	if !foundPartID.Valid && mpn != "" {
		row, err := h.Queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
			Mpn: &mpn, TeamID: sess.TeamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
		}
	}

	slog.Debug("bulk scan: db lookup", "session_id", sessionID.String(), "found", foundPartID.Valid, "has_inventory", hasInventory)

	// Determine task type for known-part paths.
	var taskType *string
	if foundPartID.Valid {
		tt := "known_part_stock_adjustment"
		if !hasInventory {
			tt = "known_part_create_entry"
		}
		taskType = &tt
	} else if sku == "" && mpn == "" {
		// No identifiers at all — unrecognized barcode.
		tt := "unrecognized"
		taskType = &tt
	}

	// Insert the item (ON CONFLICT DO NOTHING for dedup).
	item, insertErr := h.Queries.InsertBulkScanItem(ctx, dbgen.InsertBulkScanItemParams{
		SessionID:    sessionID,
		Barcode:      body.Barcode,
		Status:       "pending_review",
		TaskType:     taskType,
		PartID:       foundPartID,
		ParsedFields: parsedFields,
		InitialQty:   encodedQty,
	})

	newItem := insertErr == nil && item.ID.Valid
	slog.Debug("bulk scan: item inserted", "session_id", sessionID.String(), "new", newItem, "item_id", item.ID.String())

	if !newItem {
		// Duplicate barcode — return existing item info.
		existing, err := h.Queries.GetActiveBulkScanItemByBarcode(ctx, dbgen.GetActiveBulkScanItemByBarcodeParams{
			SessionID: sessionID,
			Barcode:   body.Barcode,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		var existingResolveID *string
		if existing.ResolveItemID.Valid {
			s := existing.ResolveItemID.String()
			existingResolveID = &s
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"item_id":         existing.ID.String(),
			"new":             false,
			"found_in_db":     foundPartID.Valid,
			"task_type":       existing.TaskType,
			"encoded_qty":     encodedQty,
			"resolve_item_id": existingResolveID,
		})
		return
	}

	// Update last_item_at on the session.
	h.Queries.UpdateBulkScanSessionLastItemAt(ctx, sessionID) //nolint:errcheck

	// For unknown parts with identifiers, kick off the resolve job.
	var resolveItemID *string
	if taskType == nil {
		// Unknown part — needs distributor fan-out.
		inputFields, _ := json.Marshal(map[string]string{
			"sku":            sku,
			"distributor_id": resolvedDistributorID.String(),
			"mpn":            mpn,
			"manufacturer":   manufacturer,
		})
		rid, err := h.Worker.CreateAndEnqueueResolveItem(ctx, sess.TeamID, sess.UserID, item.ID, inputFields)
		if err != nil {
			slog.Warn("bulk scan: failed to create resolve item", "item_id", item.ID.String(), "err", err)
		} else {
			s := rid.String()
			resolveItemID = &s
		}
	}

	// Notify desktop reviewers (who are usually NOT the scanning user) that a new
	// item landed, so the review list / badge update live instead of polling.
	// Team-wide with no actor-skip: the scanner is on the phone (which doesn't
	// receive these worker events), so even the scanning user's own desktop should
	// update.
	d, _ := json.Marshal(map[string]string{
		"session_id": sessionID.String(),
		"item_id":    item.ID.String(),
	})
	h.Worker.Publish(worker.Event{
		Type:   "bulk_scan_changed",
		TeamID: sess.TeamID.String(),
		Data:   d,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":         item.ID.String(),
		"new":             true,
		"found_in_db":     foundPartID.Valid,
		"task_type":       taskType,
		"encoded_qty":     encodedQty,
		"resolve_item_id": resolveItemID,
	})
}

// PatchBulkScanItem handles PATCH /api/v1/scan/bulk/sessions/{sessionId}/items/{itemId}.
// Sets the initial_qty captured on the phone.
func (h *Handler) PatchBulkScanItem(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	var sessionID pgtype.UUID
	if err := sessionID.Scan(chi.URLParam(r, "sessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id", "BAD_REQUEST")
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	if !h.requireBulkSessionOwned(w, r, sessionID, sess) {
		return
	}

	var body struct {
		InitialQty *int32 `json:"initial_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if err := h.Queries.SetBulkScanItemInitialQty(r.Context(), dbgen.SetBulkScanItemInitialQtyParams{
		ID:         itemID,
		InitialQty: body.InitialQty,
		SessionID:  sessionID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update item", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CloseBulkScanSessionHandler handles POST /api/v1/scan/bulk/sessions/{sessionId}/close.
func (h *Handler) CloseBulkScanSessionHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	var sessionID pgtype.UUID
	if err := sessionID.Scan(chi.URLParam(r, "sessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id", "BAD_REQUEST")
		return
	}

	if err := h.Queries.CloseBulkScanSession(r.Context(), dbgen.CloseBulkScanSessionParams{
		ID:     sessionID,
		UserID: sess.UserID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close session", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// NavigateBulkSession handles POST /api/v1/scan/bulk/sessions/{sessionId}/navigate.
func (h *Handler) NavigateBulkSession(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}

	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required", "BAD_REQUEST")
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
		"entity_type": "bulk_scan_session",
		"entity_id":   sessionID,
	})
	h.Worker.Publish(worker.Event{
		Type:         "scan_focus_requested",
		TeamID:       sess.TeamID.String(),
		TargetUserID: sess.UserID.String(),
		Data:         d,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── Desktop handlers (JWT auth) ───────────────────────────────────────────────

// ListBulkScanSessions handles GET /api/v1/teams/{teamId}/bulk-scan-sessions.
func (h *Handler) ListBulkScanSessions(w http.ResponseWriter, r *http.Request) {
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

	includeCompleted := r.URL.Query().Get("include_completed") == "true"

	sessions, err := h.Queries.ListBulkScanSessionsForUser(r.Context(), dbgen.ListBulkScanSessionsForUserParams{
		UserID:           userID,
		TeamID:           teamID,
		IncludeCompleted: includeCompleted,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type countsOut struct {
		Pending   int64 `json:"pending"`
		Reviewed  int64 `json:"reviewed"`
		Dismissed int64 `json:"dismissed"`
		Ready     int64 `json:"ready"`
	}
	type sessionItem struct {
		ID        string    `json:"id"`
		CreatedAt string    `json:"created_at"`
		ClosedAt  *string   `json:"closed_at"`
		Counts    countsOut `json:"counts"`
	}

	out := make([]sessionItem, 0, len(sessions))
	for _, s := range sessions {
		si := sessionItem{
			ID:        s.ID.String(),
			CreatedAt: s.CreatedAt.Time.Format(time.RFC3339),
			Counts: countsOut{
				Pending:   s.CountPending,
				Reviewed:  s.CountReviewed,
				Dismissed: s.CountDismissed,
				Ready:     s.CountReady,
			},
		}
		if s.ClosedAt.Valid {
			t := s.ClosedAt.Time.Format(time.RFC3339)
			si.ClosedAt = &t
		}
		out = append(out, si)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// GetBulkScanBadgeCount handles GET /api/v1/teams/{teamId}/bulk-scan-badge.
func (h *Handler) GetBulkScanBadgeCount(w http.ResponseWriter, r *http.Request) {
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

	count, err := h.Queries.GetBulkScanBadgeCount(r.Context(), dbgen.GetBulkScanBadgeCountParams{
		UserID: userID,
		TeamID: teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

// ListBulkScanItemsHandler handles GET /api/v1/teams/{teamId}/bulk-scan-sessions/{sessionId}/items.
func (h *Handler) ListBulkScanItemsHandler(w http.ResponseWriter, r *http.Request) {
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

	// Verify session belongs to team.
	_, err := h.Queries.GetBulkScanSession(r.Context(), dbgen.GetBulkScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items, err := h.Queries.ListBulkScanItemsWithResolveData(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type resolveItemOut struct {
		ID             string          `json:"id"`
		Status         string          `json:"status"`
		TaskType       *string         `json:"task_type"`
		EnrichmentData json.RawMessage `json:"enrichment_data,omitempty"`
		ErrorMessage   *string         `json:"error_message,omitempty"`
		EnrichedAt     *string         `json:"enriched_at,omitempty"`
	}
	type partInfo struct {
		MPN          *string `json:"mpn"`
		Manufacturer *string `json:"manufacturer"`
		Description  *string `json:"description"`
	}
	type itemOut struct {
		ID           string          `json:"id"`
		SessionID    string          `json:"session_id"`
		Barcode      string          `json:"barcode"`
		Status       string          `json:"status"`
		TaskType     *string         `json:"task_type"`
		ParsedFields json.RawMessage `json:"parsed_fields"`
		InitialQty   *int32          `json:"initial_qty"`
		PartID       *string         `json:"part_id"`
		Part         *partInfo       `json:"part"`
		ResolveItem  *resolveItemOut `json:"resolve_item"`
		CreatedAt    string          `json:"created_at"`
	}

	out := make([]itemOut, 0, len(items))
	for _, it := range items {
		io := itemOut{
			ID:           it.ID.String(),
			SessionID:    it.SessionID.String(),
			Barcode:      it.Barcode,
			Status:       it.Status,
			TaskType:     it.TaskType,
			ParsedFields: json.RawMessage(it.ParsedFields),
			InitialQty:   it.InitialQty,
			CreatedAt:    it.CreatedAt.Time.Format(time.RFC3339),
		}
		if it.PartID.Valid {
			s := it.PartID.String()
			io.PartID = &s
			io.Part = &partInfo{
				MPN:          it.PartMpn,
				Manufacturer: it.PartManufacturer,
				Description:  it.PartDescription,
			}
		}
		if it.ResolveItemIDCol.Valid && it.ResolveStatus != nil {
			ri := &resolveItemOut{
				ID:           it.ResolveItemIDCol.String(),
				Status:       *it.ResolveStatus,
				TaskType:     it.ResolveTaskType,
				ErrorMessage: it.ResolveErrorMessage,
			}
			if len(it.ResolveEnrichmentData) > 0 {
				ri.EnrichmentData = json.RawMessage(it.ResolveEnrichmentData)
			}
			if it.ResolveEnrichedAt.Valid {
				t := it.ResolveEnrichedAt.Time.Format(time.RFC3339)
				ri.EnrichedAt = &t
			}
			io.ResolveItem = ri
		}
		out = append(out, io)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// SetBulkScanItemStatusHandler handles PATCH /api/v1/teams/{teamId}/bulk-scan-sessions/{sessionId}/items/{itemId}.
// Accepts status "reviewed" or "dismissed". Optional part_id sets the resolved part on review.
func (h *Handler) SetBulkScanItemStatusHandler(w http.ResponseWriter, r *http.Request) {
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
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}

	var body struct {
		Status string `json:"status"`
		PartID string `json:"part_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Status != "reviewed" && body.Status != "dismissed" {
		writeError(w, http.StatusBadRequest, "status must be reviewed or dismissed", "BAD_REQUEST")
		return
	}

	// Verify session belongs to team.
	_, err := h.Queries.GetBulkScanSession(r.Context(), dbgen.GetBulkScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Scope guard: the item must belong to the validated session. Without this the
	// session-team check is bypassable — an attacker passes their own session id plus
	// another team's/session's item id and mutates it.
	item, err := h.Queries.GetBulkScanItem(r.Context(), itemID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.SessionID != sessionID) {
		writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	var userID string
	if user != nil {
		userID = user.ID
	}

	var updatedID pgtype.UUID
	if body.Status == "reviewed" {
		var partID pgtype.UUID
		if body.PartID != "" {
			partID.Scan(body.PartID) //nolint:errcheck
		}
		updated, err := h.Queries.SetBulkScanItemReviewed(r.Context(), dbgen.SetBulkScanItemReviewedParams{
			ID:     itemID,
			PartID: partID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "item already processed", "CONFLICT")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		updatedID = updated.ID
	} else {
		updated, err := h.Queries.SetBulkScanItemDismissed(r.Context(), itemID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "item already processed", "CONFLICT")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		updatedID = updated.ID
	}

	// Publish SSE so other open tabs / desktop reviewers update. Deliver
	// team-wide (skipping only the acting reviewer's own channel) — the reviewer
	// is frequently NOT the user who scanned, so this must not be user-scoped.
	d, _ := json.Marshal(map[string]string{
		"session_id": sessionID.String(),
		"item_id":    updatedID.String(),
	})
	h.Worker.Publish(worker.Event{
		Type:   "bulk_scan_changed",
		TeamID: teamID.String(),
		UserID: userID,
		Data:   d,
	})

	w.WriteHeader(http.StatusNoContent)
}

// RetryBulkScanItemHandler handles POST .../items/{itemId}/retry.
// Resets the linked resolve item to pending and re-enqueues the resolve job.
func (h *Handler) RetryBulkScanItemHandler(w http.ResponseWriter, r *http.Request) {
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
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}

	if _, err := h.Queries.GetBulkScanSession(r.Context(), dbgen.GetBulkScanSessionParams{
		ID: sessionID, TeamID: teamID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return
	}

	item, err := h.Queries.GetBulkScanItem(r.Context(), itemID)
	// Scope guard: the item must belong to the validated session (the session-team
	// check alone is bypassable with a foreign item id).
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.SessionID != sessionID) {
		writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if !item.ResolveItemID.Valid {
		writeError(w, http.StatusUnprocessableEntity, "item has no linked resolve job", "NO_RESOLVE_ITEM")
		return
	}

	if err := h.Queries.UpdatePartResolveItemRetryPending(r.Context(), item.ResolveItemID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset resolve item", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	userIDStr := ""
	if user != nil {
		userIDStr = user.ID
	}

	jobID := h.Worker.Enqueue(worker.JobPartResolve, teamID.String(), map[string]string{
		"resolve_item_id": item.ResolveItemID.String(),
		"team_id":         teamID.String(),
		"user_id":         userIDStr,
	})

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// RetryBulkScanItemDistributorsHandler handles POST .../items/{itemId}/retry-distributors.
// Re-queries a specific subset of distributors on the linked resolve item.
func (h *Handler) RetryBulkScanItemDistributorsHandler(w http.ResponseWriter, r *http.Request) {
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
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}

	var body struct {
		DistributorIDs []string `json:"distributor_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.DistributorIDs) == 0 {
		writeError(w, http.StatusBadRequest, "distributor_ids required", "BAD_REQUEST")
		return
	}

	if _, err := h.Queries.GetBulkScanSession(r.Context(), dbgen.GetBulkScanSessionParams{
		ID: sessionID, TeamID: teamID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return
	}

	item, err := h.Queries.GetBulkScanItem(r.Context(), itemID)
	// Scope guard: the item must belong to the validated session (the session-team
	// check alone is bypassable with a foreign item id).
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.SessionID != sessionID) {
		writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if !item.ResolveItemID.Valid {
		writeError(w, http.StatusUnprocessableEntity, "item has no linked resolve job", "NO_RESOLVE_ITEM")
		return
	}

	resolveItem, err := h.Queries.GetPartResolveItem(r.Context(), item.ResolveItemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if resolveItem.Status != "enriched" && resolveItem.Status != "failed" {
		writeError(w, http.StatusUnprocessableEntity, "resolve item not eligible for retry", "NOT_ELIGIBLE")
		return
	}

	if err := h.Queries.UpdatePartResolveItemRetryPending(r.Context(), item.ResolveItemID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset resolve item", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	userIDStr := ""
	if user != nil {
		userIDStr = user.ID
	}

	jobID := h.Worker.Enqueue(worker.JobPartResolve, teamID.String(), map[string]string{
		"resolve_item_id":       item.ResolveItemID.String(),
		"team_id":               teamID.String(),
		"user_id":               userIDStr,
		"retry_distributor_ids": strings.Join(body.DistributorIDs, ","),
	})

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// DeleteBulkScanSessionHandler handles DELETE /api/v1/teams/{teamId}/bulk-scan-sessions/{sessionId}.
func (h *Handler) DeleteBulkScanSessionHandler(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Queries.SoftDeleteBulkScanSession(r.Context(), dbgen.SoftDeleteBulkScanSessionParams{
		ID:     sessionID,
		TeamID: teamID,
		UserID: userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

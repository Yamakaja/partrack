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

// ── Phone handlers (scan token auth) ─────────────────────────────────────────

// StartBulkSession handles POST /api/v1/scan/bulk/start.
// Creates a new bulk scan session for the current scan-token user.
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
// Inserts the scanned barcode into the session (deduped by unique constraint),
// kicks off background enrichment, and returns whether the part already exists.
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

	// Resolve the parsed distributor short-name to its canonical DB name and ID
	// by doing a case-insensitive substring match against the team's enabled
	// distributor configs. This handles mismatches like parser "LCSC" vs DB
	// "LCSC / JLCPCB". The canonical name and ID are stored in parsed_fields so
	// the enrichment worker can match by UUID rather than by name.
	var resolvedDistributorName string
	var resolvedDistributorID string
	if parseErr == nil && parsed != nil && parsed.Distributor != "" {
		if cfgs, err := h.Queries.ListEnabledDistributorConfigsForTeam(ctx, sess.TeamID); err == nil {
			lowerParsed := strings.ToLower(parsed.Distributor)
			for _, cfg := range cfgs {
				if strings.Contains(strings.ToLower(cfg.DistributorName), lowerParsed) {
					resolvedDistributorName = cfg.DistributorName
					resolvedDistributorID = cfg.DistributorID.String()
					break
				}
			}
		}
		if resolvedDistributorName != "" {
			slog.Debug("bulk scan: distributor resolved", "session_id", sessionID.String(), "parsed_name", parsed.Distributor, "canonical_name", resolvedDistributorName, "distributor_id", resolvedDistributorID)
		} else {
			slog.Debug("bulk scan: distributor not resolved", "session_id", sessionID.String(), "parsed_name", parsed.Distributor)
		}
	}

	// Build parsed_fields JSON to store alongside the item.
	var parsedFields []byte
	var encodedQty *int
	if parseErr == nil && parsed != nil {
		distName := resolvedDistributorName
		if distName == "" {
			distName = parsed.Distributor
		}
		pf := map[string]any{
			"distributor":    distName,
			"distributor_id": resolvedDistributorID,
			"sku":            parsed.SKU,
			"mpn":            parsed.MPN,
			"manufacturer":   parsed.Manufacturer,
			"encoded_qty":    parsed.Quantity,
		}
		parsedFields, _ = json.Marshal(pf)
		if parsed.Quantity > 0 {
			q := parsed.Quantity
			encodedQty = &q
		}
	}

	// Synchronous DB lookup to determine found_in_db for the phone response.
	distNameForLookup := resolvedDistributorName
	if distNameForLookup == "" && parsed != nil {
		distNameForLookup = parsed.Distributor
	}
	foundInDB := false
	if parseErr == nil && parsed != nil {
		if parsed.SKU != "" && distNameForLookup != "" {
			_, lookErr := h.Queries.GetPartBySKUAndDistributorWithInventory(ctx, dbgen.GetPartBySKUAndDistributorWithInventoryParams{
				Sku: parsed.SKU, TeamID: sess.TeamID, DistributorName: distNameForLookup,
			})
			if lookErr == nil {
				foundInDB = true
			}
		}
		if !foundInDB && parsed.SKU != "" {
			_, lookErr := h.Queries.GetPartBySKUWithInventory(ctx, dbgen.GetPartBySKUWithInventoryParams{
				Sku: parsed.SKU, TeamID: sess.TeamID,
			})
			if lookErr == nil {
				foundInDB = true
			}
		}
		if !foundInDB && parsed.MPN != "" && parsed.Manufacturer != "" {
			_, lookErr := h.Queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
				Mpn: &parsed.MPN, Manufacturer: &parsed.Manufacturer, TeamID: sess.TeamID,
			})
			if lookErr == nil {
				foundInDB = true
			}
		}
		if !foundInDB && parsed.MPN != "" {
			_, lookErr := h.Queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
				Mpn: &parsed.MPN, TeamID: sess.TeamID,
			})
			if lookErr == nil {
				foundInDB = true
			}
		}
	}

	slog.Debug("bulk scan: db lookup result", "session_id", sessionID.String(), "found_in_db", foundInDB)

	// Insert the item (ON CONFLICT DO NOTHING).
	item, insertErr := h.Queries.InsertBulkScanItem(ctx, dbgen.InsertBulkScanItemParams{
		SessionID:    sessionID,
		Barcode:      body.Barcode,
		ParsedFields: parsedFields,
	})

	newItem := insertErr == nil && item.ID.Valid
	slog.Debug("bulk scan: item inserted", "session_id", sessionID.String(), "new", newItem, "item_id", item.ID.String())

	if newItem {
		// Update last_item_at on the session.
		h.Queries.UpdateBulkScanSessionLastItemAt(ctx, sessionID) //nolint:errcheck

		// Enqueue background enrichment.
		jobID := h.Worker.Enqueue(worker.JobBulkScanEnrich, sess.TeamID.String(), map[string]string{
			"item_id":    item.ID.String(),
			"session_id": sessionID.String(),
			"user_id":    sess.UserID.String(),
			"team_id":    sess.TeamID.String(),
		})

		// Store the job ID back on the item (best effort).
		var jobUUID pgtype.UUID
		if err := jobUUID.Scan(jobID); err == nil {
			h.Queries.SetBulkScanItemEnrichmentJobID(ctx, dbgen.SetBulkScanItemEnrichmentJobIDParams{ //nolint:errcheck
				ID:              item.ID,
				EnrichmentJobID: jobUUID,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"item_id":      item.ID.String(),
			"new":          true,
			"found_in_db":  foundInDB,
			"encoded_qty":  encodedQty,
		})
		return
	}

	// Item already existed (dedup) — return the existing item's ID.
	existing, err := h.Queries.GetActiveBulkScanItemByBarcode(ctx, dbgen.GetActiveBulkScanItemByBarcodeParams{
		SessionID: sessionID,
		Barcode:   body.Barcode,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":     existing.ID.String(),
		"new":         false,
		"found_in_db": foundInDB,
		"encoded_qty": encodedQty,
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
// Pushes a scan_focus_requested event to the desktop so it navigates to the review page.
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

	type sessionItem struct {
		ID        string  `json:"id"`
		CreatedAt string  `json:"created_at"`
		ClosedAt  *string `json:"closed_at"`
		Counts    struct {
			Pending   int64 `json:"pending_enrichment"`
			Enriched  int64 `json:"enriched"`
			Error     int64 `json:"error"`
			Reviewed  int64 `json:"reviewed"`
			Dismissed int64 `json:"dismissed"`
		} `json:"counts"`
	}

	out := make([]sessionItem, 0, len(sessions))
	for _, s := range sessions {
		si := sessionItem{
			ID:        s.ID.String(),
			CreatedAt: s.CreatedAt.Time.Format(time.RFC3339),
		}
		if s.ClosedAt.Valid {
			t := s.ClosedAt.Time.Format(time.RFC3339)
			si.ClosedAt = &t
		}
		si.Counts.Pending = s.CountPending
		si.Counts.Enriched = s.CountEnriched
		si.Counts.Error = s.CountError
		si.Counts.Reviewed = s.CountReviewed
		si.Counts.Dismissed = s.CountDismissed
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

	count, err := h.Queries.GetBulkScanSessionBadgeCount(r.Context(), dbgen.GetBulkScanSessionBadgeCountParams{
		UserID: userID,
		TeamID: teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int32{"count": count})
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

	items, err := h.Queries.ListBulkScanItems(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type partInfo struct {
		MPN          *string `json:"mpn"`
		Manufacturer *string `json:"manufacturer"`
		Description  *string `json:"description"`
	}
	type itemOut struct {
		ID             string           `json:"id"`
		SessionID      string           `json:"session_id"`
		Barcode        string           `json:"barcode"`
		Status         string           `json:"status"`
		TaskType       *string          `json:"task_type"`
		ParsedFields   json.RawMessage  `json:"parsed_fields"`
		InitialQty     *int32           `json:"initial_qty"`
		PartID         *string          `json:"part_id"`
		Part           *partInfo        `json:"part"`
		EnrichmentData json.RawMessage  `json:"enrichment_data"`
		ErrorMessage   *string          `json:"error_message"`
		CreatedAt      string           `json:"created_at"`
		EnrichedAt     *string          `json:"enriched_at"`
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
			ErrorMessage: it.ErrorMessage,
			CreatedAt:    it.CreatedAt.Time.Format(time.RFC3339),
		}
		if len(it.EnrichmentData) > 0 {
			io.EnrichmentData = json.RawMessage(it.EnrichmentData)
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
		if it.EnrichedAt.Valid {
			t := it.EnrichedAt.Time.Format(time.RFC3339)
			io.EnrichedAt = &t
		}
		out = append(out, io)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// SetBulkScanItemStatusHandler handles PATCH /api/v1/teams/{teamId}/bulk-scan-sessions/{sessionId}/items/{itemId}.
// Sets status to reviewed or dismissed. First-write-wins: returns 409 if already terminal.
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Status != "reviewed" && body.Status != "dismissed" {
		writeError(w, http.StatusBadRequest, "status must be reviewed or dismissed", "BAD_REQUEST")
		return
	}

	// Verify session belongs to team first.
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

	updated, err := h.Queries.SetBulkScanItemStatus(r.Context(), dbgen.SetBulkScanItemStatusParams{
		ID:        itemID,
		Status:    body.Status,
		SessionID: sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Item already reviewed/dismissed (first-write-wins).
		writeError(w, http.StatusConflict, "item already processed", "CONFLICT")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Publish SSE so other open tabs / desktop update.
	d, _ := json.Marshal(map[string]string{
		"session_id": sessionID.String(),
		"item_id":    updated.ID.String(),
	})
	user := middleware.UserFromContext(r.Context())
	var userID string
	if user != nil {
		userID = user.ID
	}
	h.Worker.Publish(worker.Event{
		Type:         "bulk_scan_changed",
		TeamID:       teamID.String(),
		TargetUserID: userID,
		Data:         d,
	})

	w.WriteHeader(http.StatusNoContent)
}

// RetryBulkScanItemHandler handles POST /api/v1/teams/{teamId}/bulk-scan-sessions/{sessionId}/items/{itemId}/retry.
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

	// Verify session belongs to team.
	sess, err := h.Queries.GetBulkScanSession(r.Context(), dbgen.GetBulkScanSessionParams{
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

	// Get item to retrieve parsed fields for the job payload.
	item, err := h.Queries.GetBulkScanItem(r.Context(), itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	userIDStr := ""
	if user != nil {
		userIDStr = user.ID
	}

	jobID := h.Worker.Enqueue(worker.JobBulkScanEnrich, teamID.String(), map[string]string{
		"item_id":    item.ID.String(),
		"session_id": sessionID.String(),
		"user_id":    userIDStr,
		"team_id":    sess.TeamID.String(),
	})

	var jobUUID pgtype.UUID
	if err := jobUUID.Scan(jobID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue job", "INTERNAL_ERROR")
		return
	}

	_, err = h.Queries.SetBulkScanItemRetry(r.Context(), dbgen.SetBulkScanItemRetryParams{
		ID:              itemID,
		EnrichmentJobID: jobUUID,
		SessionID:       sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "item is not in error state", "CONFLICT")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// RetryBulkScanItemDistributorsHandler handles POST .../items/{itemId}/retry-distributors.
// Re-runs enrichment for a specific subset of distributors on an already-enriched
// item, merging new results into the existing enrichment_data without resetting status.
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

	user := middleware.UserFromContext(r.Context())
	userIDStr := ""
	if user != nil {
		userIDStr = user.ID
	}

	jobID := h.Worker.Enqueue(worker.JobBulkScanEnrich, teamID.String(), map[string]string{
		"item_id":               itemID.String(),
		"session_id":            sessionID.String(),
		"user_id":               userIDStr,
		"team_id":               teamID.String(),
		"retry_distributor_ids": strings.Join(body.DistributorIDs, ","),
	})

	// Reset item to pending_enrichment immediately so the UI shows a spinner
	// while the retry job is in flight.
	var jobUUID pgtype.UUID
	if err := jobUUID.Scan(jobID); err == nil {
		if _, err := h.Queries.SetBulkScanItemDistributorRetryPending(r.Context(), dbgen.SetBulkScanItemDistributorRetryPendingParams{
			ID:              itemID,
			EnrichmentJobID: jobUUID,
		}); err == nil {
			data, _ := json.Marshal(map[string]string{
				"session_id": sessionID.String(),
				"item_id":    itemID.String(),
			})
			h.Worker.Publish(worker.Event{
				Type:         "bulk_scan_changed",
				TeamID:       teamID.String(),
				TargetUserID: userIDStr,
				Data:         data,
			})
		}
	}

	w.WriteHeader(http.StatusAccepted)
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

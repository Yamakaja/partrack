package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CreatePartResolveItem creates a resolution job for one set of part identifiers
// and returns immediately. The worker fans out distributor API calls in the
// background and publishes a part_resolve_changed SSE event when done.
//
// POST /api/v1/teams/{teamId}/parts/resolve
func (h *Handler) CreatePartResolveItem(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "invalid_team_id")
		return
	}

	var body struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
		MPN           string `json:"mpn"`
		Manufacturer  string `json:"manufacturer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_body")
		return
	}

	hasSKU := body.SKU != "" && body.DistributorID != ""
	hasMPN := body.MPN != ""
	if !hasSKU && !hasMPN {
		writeError(w, http.StatusUnprocessableEntity,
			"provide sku+distributor_id or mpn", "missing_identifiers")
		return
	}

	userID := userIDFromContext(r)
	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}

	inputFields, _ := json.Marshal(map[string]string{
		"sku":            body.SKU,
		"distributor_id": body.DistributorID,
		"mpn":            body.MPN,
		"manufacturer":   body.Manufacturer,
	})

	item, err := h.Queries.CreatePartResolveItem(r.Context(), dbgen.CreatePartResolveItemParams{
		TeamID:      teamID,
		SourceType:  "manual",
		InputFields: inputFields,
		CreatedBy:   userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create resolve item", "db_error")
		return
	}

	jobID := h.Worker.Enqueue(worker.JobPartResolve, teamID.String(), map[string]string{
		"resolve_item_id": item.ID.String(),
		"team_id":         teamID.String(),
		"user_id":         userID,
	})

	writeJSON(w, http.StatusAccepted, map[string]string{
		"resolve_item_id": item.ID.String(),
		"job_id":          jobID,
	})
}

// GetPartResolveItem returns the current status and enrichment results of a
// resolution item. The frontend polls this or responds to the SSE event.
//
// GET /api/v1/teams/{teamId}/parts/resolve/{resolveItemId}
func (h *Handler) GetPartResolveItem(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "invalid_team_id")
		return
	}
	var resolveItemID pgtype.UUID
	if err := resolveItemID.Scan(chi.URLParam(r, "resolveItemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid resolve item ID", "invalid_resolve_item_id")
		return
	}

	item, err := h.Queries.GetPartResolveItem(r.Context(), resolveItemID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resolve item not found", "not_found")
		return
	}
	if item.TeamID != teamID {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}

	type response struct {
		ID             string          `json:"id"`
		Status         string          `json:"status"`
		TaskType       *string         `json:"task_type"`
		SourceType     string          `json:"source_type"`
		SourceID       *string         `json:"source_id,omitempty"`
		ResolvedPartID *string         `json:"resolved_part_id,omitempty"`
		EnrichmentData json.RawMessage `json:"enrichment_data,omitempty"`
		ErrorMessage   *string         `json:"error_message,omitempty"`
		EnrichedAt     *string         `json:"enriched_at,omitempty"`
		CommittedAt    *string         `json:"committed_at,omitempty"`
	}

	resp := response{
		ID:             item.ID.String(),
		Status:         item.Status,
		TaskType:       item.TaskType,
		SourceType:     item.SourceType,
		EnrichmentData: json.RawMessage(item.EnrichmentData),
		ErrorMessage:   item.ErrorMessage,
	}
	if item.SourceID.Valid {
		s := item.SourceID.String()
		resp.SourceID = &s
	}
	if item.ResolvedPartID.Valid {
		s := item.ResolvedPartID.String()
		resp.ResolvedPartID = &s
	}
	if item.EnrichedAt.Valid {
		s := item.EnrichedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		resp.EnrichedAt = &s
	}
	if item.CommittedAt.Valid {
		s := item.CommittedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		resp.CommittedAt = &s
	}

	writeJSON(w, http.StatusOK, resp)
}

// CommitPartResolveItem writes the cached enrichment results into parts,
// distributor_listings, and inventory_entries without making any API calls.
//
// POST /api/v1/teams/{teamId}/parts/resolve/{resolveItemId}/commit
func (h *Handler) CommitPartResolveItem(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "invalid_team_id")
		return
	}
	var resolveItemID pgtype.UUID
	if err := resolveItemID.Scan(chi.URLParam(r, "resolveItemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid resolve item ID", "invalid_resolve_item_id")
		return
	}

	var body struct {
		PrimaryDistributorID string `json:"primary_distributor_id"`
		PrimarySKU           string `json:"primary_sku"`
		AdditionalListings   []struct {
			DistributorID string `json:"distributor_id"`
			SKU           string `json:"sku"`
		} `json:"additional_listings"`
		InitialQty        *int32 `json:"initial_qty"`
		InitialQtyReason  string `json:"initial_qty_reason"`
		StorageLocationID string `json:"storage_location_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "invalid_body")
		return
	}

	var storageLocID pgtype.UUID
	if body.StorageLocationID != "" {
		storageLocID.Scan(body.StorageLocationID) //nolint:errcheck
	}

	additionalListings := make([]worker.AdditionalListing, len(body.AdditionalListings))
	for i, al := range body.AdditionalListings {
		additionalListings[i] = worker.AdditionalListing{DistributorID: al.DistributorID, SKU: al.SKU}
	}

	userID := userIDFromContext(r)

	partID, err := h.Worker.CommitResolveItem(
		r.Context(),
		teamID,
		resolveItemID,
		body.PrimaryDistributorID,
		body.PrimarySKU,
		additionalListings,
		userID,
		body.InitialQty,
		body.InitialQtyReason,
		storageLocID,
	)
	if err != nil {
		switch {
		case errors.Is(err, worker.ErrResolveItemConflict):
			writeError(w, http.StatusConflict, "already committed", "already_committed")
		case errors.Is(err, worker.ErrResolveItemNotReady):
			writeError(w, http.StatusUnprocessableEntity, "item not ready for commit", "not_ready")
		case errors.Is(err, worker.ErrResolveItemUnrecognized):
			writeError(w, http.StatusUnprocessableEntity, "cannot commit unrecognized item", "unrecognized")
		case errors.Is(err, worker.ErrResolveItemNoResult):
			writeError(w, http.StatusUnprocessableEntity, "primary distributor has no result", "no_result")
		default:
			writeError(w, http.StatusInternalServerError, "commit failed: "+err.Error(), "commit_failed")
		}
		return
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"part_id": partID.String()})
}

// RetryPartResolveDistributors re-queries a specific set of distributors
// (those that returned errors) and merges fresh results into the existing
// enrichment_data without touching entries that already succeeded.
//
// POST /api/v1/teams/{teamId}/parts/resolve/{resolveItemId}/retry-distributors
func (h *Handler) RetryPartResolveDistributors(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "invalid_team_id")
		return
	}
	var resolveItemID pgtype.UUID
	if err := resolveItemID.Scan(chi.URLParam(r, "resolveItemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid resolve item ID", "invalid_resolve_item_id")
		return
	}

	var body struct {
		DistributorIDs []string `json:"distributor_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.DistributorIDs) == 0 {
		writeError(w, http.StatusBadRequest, "distributor_ids must be a non-empty array", "invalid_body")
		return
	}

	item, err := h.Queries.GetPartResolveItem(r.Context(), resolveItemID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resolve item not found", "not_found")
		return
	}
	if item.TeamID != teamID {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	switch item.Status {
	case "committed":
		writeError(w, http.StatusConflict, "item already committed", "already_committed")
		return
	case "pending":
		writeError(w, http.StatusUnprocessableEntity, "item is still being resolved", "not_ready")
		return
	}

	if err := h.Queries.UpdatePartResolveItemRetryPending(r.Context(), resolveItemID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset item status", "db_error")
		return
	}

	userID := userIDFromContext(r)
	jobID := h.Worker.Enqueue(worker.JobPartResolve, teamID.String(), map[string]string{
		"resolve_item_id":       resolveItemID.String(),
		"team_id":               teamID.String(),
		"user_id":               userID,
		"retry_distributor_ids": strings.Join(body.DistributorIDs, ","),
	})

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

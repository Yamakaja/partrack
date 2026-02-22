package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListStorageLocations handles GET /api/v1/teams/{teamId}/storage-locations
func (h *Handler) ListStorageLocations(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	locations, err := h.Queries.ListStorageLocations(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID          string  `json:"id"`
		TeamID      string  `json:"team_id"`
		Label       string  `json:"label"`
		Description *string `json:"description"`
	}
	items := make([]item, len(locations))
	for i, loc := range locations {
		items[i] = item{
			ID:          loc.ID.String(),
			TeamID:      loc.TeamID.String(),
			Label:       loc.Label,
			Description: loc.Description,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateStorageLocation handles POST /api/v1/teams/{teamId}/storage-locations
func (h *Handler) CreateStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Label       string  `json:"label"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required", "BAD_REQUEST")
		return
	}

	loc, err := h.Queries.CreateStorageLocation(r.Context(), dbgen.CreateStorageLocationParams{
		TeamID:      teamID,
		Label:       req.Label,
		Description: req.Description,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a storage location with this label already exists", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          loc.ID.String(),
		"team_id":     loc.TeamID.String(),
		"label":       loc.Label,
		"description": loc.Description,
	})
}

// UpdateStorageLocation handles PATCH /api/v1/teams/{teamId}/storage-locations/{locationId}
func (h *Handler) UpdateStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID, locationID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := locationID.Scan(chi.URLParam(r, "locationId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Label       *string `json:"label"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	loc, err := h.Queries.UpdateStorageLocation(r.Context(), dbgen.UpdateStorageLocationParams{
		ID:          locationID,
		TeamID:      teamID,
		Label:       req.Label,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "storage location not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          loc.ID.String(),
		"team_id":     loc.TeamID.String(),
		"label":       loc.Label,
		"description": loc.Description,
	})
}

// DeleteStorageLocation handles DELETE /api/v1/teams/{teamId}/storage-locations/{locationId}
func (h *Handler) DeleteStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID, locationID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := locationID.Scan(chi.URLParam(r, "locationId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location ID", "BAD_REQUEST")
		return
	}

	count, err := h.Queries.CountInventoryEntriesForLocation(r.Context(), locationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if count > 0 {
		writeError(w, http.StatusUnprocessableEntity, "storage location has inventory entries", "LOCATION_IN_USE")
		return
	}

	if err := h.Queries.DeleteStorageLocation(r.Context(), dbgen.DeleteStorageLocationParams{
		ID:     locationID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

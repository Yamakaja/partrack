package handler

import (
	"encoding/json"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListDistributors handles GET /api/v1/distributors
// Lists all distributors known to the system.
func (h *Handler) ListDistributors(w http.ResponseWriter, r *http.Request) {
	distributors, err := h.Queries.ListDistributors(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Website *string `json:"website"`
		ApiType *string `json:"api_type"`
	}
	items := make([]item, len(distributors))
	for i, d := range distributors {
		items[i] = item{
			ID:      d.ID.String(),
			Name:    d.Name,
			Website: d.Website,
			ApiType: d.ApiType,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": len(items), "limit": len(items), "offset": 0, "items": items,
	})
}

// ListDistributorConfigs handles GET /api/v1/teams/{teamId}/distributor-configs
func (h *Handler) ListDistributorConfigs(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	configs, err := h.Queries.ListDistributorConfigs(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		DistributorID        string `json:"distributor_id"`
		DistributorName      string `json:"distributor_name"`
		Enabled              bool   `json:"enabled"`
		Preferred            bool   `json:"preferred"`
		CacheDurationSeconds int32  `json:"cache_duration_seconds"`
		HasCredentials       bool   `json:"has_credentials"`
	}
	items := make([]item, len(configs))
	for i, c := range configs {
		hasCreds, _ := c.HasCredentials.(bool)
		items[i] = item{
			DistributorID:        c.DistributorID.String(),
			DistributorName:      c.DistributorName,
			Enabled:              c.Enabled,
			Preferred:            c.Preferred,
			CacheDurationSeconds: c.CacheDurationSeconds,
			HasCredentials:       hasCreds,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// UpsertDistributorConfig handles PUT /api/v1/teams/{teamId}/distributor-configs/{distributorId}
// Creates or replaces the team's config for a distributor.
func (h *Handler) UpsertDistributorConfig(w http.ResponseWriter, r *http.Request) {
	var teamID, distributorID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := distributorID.Scan(chi.URLParam(r, "distributorId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid distributor ID", "BAD_REQUEST")
		return
	}

	// Decode body twice: once to detect key presence, once for typed fields.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var req struct {
		Enabled              bool  `json:"enabled"`
		Preferred            bool  `json:"preferred"`
		CacheDurationSeconds int32 `json:"cache_duration_seconds"`
	}
	body, _ := json.Marshal(raw)
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	type configResult struct {
		DistributorID        string `json:"distributor_id"`
		Enabled              bool   `json:"enabled"`
		Preferred            bool   `json:"preferred"`
		CacheDurationSeconds int32  `json:"cache_duration_seconds"`
		HasCredentials       bool   `json:"has_credentials"`
	}

	_, credentialsPresent := raw["api_credentials"]
	if !credentialsPresent {
		// Leave existing credentials unchanged.
		row, err := h.Queries.UpsertDistributorConfigKeepCredentials(r.Context(),
			dbgen.UpsertDistributorConfigKeepCredentialsParams{
				TeamID:               teamID,
				DistributorID:        distributorID,
				Enabled:              req.Enabled,
				Preferred:            req.Preferred,
				CacheDurationSeconds: req.CacheDurationSeconds,
			})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		hasCreds, _ := row.HasCredentials.(bool)
		writeJSON(w, http.StatusOK, configResult{
			DistributorID:        row.DistributorID.String(),
			Enabled:              row.Enabled,
			Preferred:            row.Preferred,
			CacheDurationSeconds: row.CacheDurationSeconds,
			HasCredentials:       hasCreds,
		})
		return
	}

	// api_credentials key was present — set to provided value (null to clear).
	var apiCredentials []byte
	if string(raw["api_credentials"]) != "null" {
		apiCredentials = []byte(raw["api_credentials"])
	}

	row, err := h.Queries.UpsertDistributorConfig(r.Context(), dbgen.UpsertDistributorConfigParams{
		TeamID:               teamID,
		DistributorID:        distributorID,
		Enabled:              req.Enabled,
		Preferred:            req.Preferred,
		CacheDurationSeconds: req.CacheDurationSeconds,
		ApiCredentials:       apiCredentials,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	hasCreds, _ := row.HasCredentials.(bool)
	writeJSON(w, http.StatusOK, configResult{
		DistributorID:        row.DistributorID.String(),
		Enabled:              row.Enabled,
		Preferred:            row.Preferred,
		CacheDurationSeconds: row.CacheDurationSeconds,
		HasCredentials:       hasCreds,
	})
}

// DeleteDistributorConfig handles DELETE /api/v1/teams/{teamId}/distributor-configs/{distributorId}
func (h *Handler) DeleteDistributorConfig(w http.ResponseWriter, r *http.Request) {
	var teamID, distributorID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := distributorID.Scan(chi.URLParam(r, "distributorId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid distributor ID", "BAD_REQUEST")
		return
	}

	if err := h.Queries.DeleteDistributorConfig(r.Context(), dbgen.DeleteDistributorConfigParams{
		TeamID:        teamID,
		DistributorID: distributorID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

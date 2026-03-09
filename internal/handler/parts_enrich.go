package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// EnrichPart handles POST /api/v1/teams/{teamId}/parts/{partId}/enrich
// Triggers enrichment from a specific distributor SKU. Returns 202 with job_id.
// Body: {sku, distributor_id} (both required).
func (h *Handler) EnrichPart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")

	var req struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SKU == "" || req.DistributorID == "" {
		writeError(w, http.StatusBadRequest, "sku and distributor_id are required", "BAD_REQUEST")
		return
	}

	if _, err := h.Queries.GetPart(r.Context(), partID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	jobID := h.enqueueEnrich(teamIDStr, partID.String(), req.SKU, req.DistributorID, user.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// ReEnrichAll handles POST /api/v1/teams/{teamId}/parts/enrich-all
// Enqueues enrichment for every part in the team's inventory that has a listing.
// Restricted to global admins only.
func (h *Handler) ReEnrichAll(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if !user.IsAdmin {
		writeError(w, http.StatusForbidden, "requires global admin", "FORBIDDEN")
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.GetPartsForReenrichment(r.Context(), teamID)
	if err != nil {
		slog.Error("GetPartsForReenrichment failed", "team_id", chi.URLParam(r, "teamId"), "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")
	for _, row := range rows {
		h.enqueueEnrich(teamIDStr, row.PartID.String(), row.Sku, row.DistributorID.String(), user.ID)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"job_count": len(rows)})
}

// enqueueEnrich enqueues a JobPartEnrich worker task and returns the job ID.
func (h *Handler) enqueueEnrich(teamID, partID, sku, distributorID, userID string) string {
	return h.Worker.Enqueue(worker.JobPartEnrich, teamID, map[string]string{
		"part_id":        partID,
		"sku":            sku,
		"distributor_id": distributorID,
		"user_id":        userID,
	})
}

// ImportPart handles POST /api/v1/teams/{teamId}/parts/import
// Atomically imports a part from a distributor SKU: finds or creates a global-scoped part,
// upserts all listings and parameters, and ensures a team inventory entry exists.
// The distributor API call is made synchronously; no job is queued.
// Body: {sku, distributor_id} (both required). Returns 200 {part_id}.
func (h *Handler) ImportPart(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SKU == "" || req.DistributorID == "" {
		writeError(w, http.StatusBadRequest, "sku and distributor_id are required", "BAD_REQUEST")
		return
	}

	configs, err := h.Queries.ListEnabledDistributorConfigsForTeam(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	var cfg *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if configs[i].DistributorID.String() == req.DistributorID {
			cfg = &configs[i]
			break
		}
	}
	if cfg == nil {
		writeError(w, http.StatusBadRequest, "distributor not configured or not enabled for this team", "BAD_REQUEST")
		return
	}
	if cfg.ApiType == nil {
		writeError(w, http.StatusBadRequest, "distributor has no API integration", "BAD_REQUEST")
		return
	}
	client := h.Distributors.Lookup(*cfg.ApiType)
	if client == nil {
		writeError(w, http.StatusInternalServerError, "no client registered for this distributor", "INTERNAL_ERROR")
		return
	}

	result, err := client.EnrichPart(r.Context(), req.SKU, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		slog.Error("ImportPart: distributor API error", "sku", req.SKU, "err", err)
		writeError(w, http.StatusBadGateway, "distributor API error: "+err.Error(), "DISTRIBUTOR_ERROR")
		return
	}
	if result == nil {
		writeError(w, http.StatusNotFound, "part not found at distributor", "NOT_FOUND")
		return
	}

	partID, err := worker.ApplyEnrichmentResult(r.Context(), h.DB, h.Queries, teamID, cfg.DistributorID, result, nil, nil)
	if err != nil {
		slog.Error("ImportPart: apply enrichment failed", "sku", req.SKU, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to import part: "+err.Error(), "INTERNAL_ERROR")
		return
	}

	// Ensure the team has an inventory entry for this part.
	_, err = h.Queries.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID,
		PartID: partID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = h.Queries.CreateInventoryEntry(r.Context(), dbgen.CreateInventoryEntryParams{
			TeamID: teamID,
			PartID: partID,
		})
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"part_id": partID.String()})
}

// ListPartCategories handles GET /api/v1/teams/{teamId}/parts/categories
// Returns the distinct set of category IDs present in the team's parts catalog.
func (h *Handler) ListPartCategories(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	rows, err := h.Queries.ListPartCategoryIDs(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	ids := make([]int32, 0, len(rows))
	for _, id := range rows {
		if id != nil {
			ids = append(ids, *id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"category_ids": ids})
}

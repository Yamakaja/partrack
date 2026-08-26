package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Yamakaja/partrack/internal/apitypes"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by importPartBySKU; callers map these to HTTP statuses.
var (
	errDistributorNotConfigured  = errors.New("distributor not configured or not enabled for this team")
	errDistributorNoAPI          = errors.New("distributor has no API integration")
	errDistributorNoClient       = errors.New("no client registered for this distributor")
	errPartNotFoundAtDistributor = errors.New("part not found at distributor")
)

// importPartBySKU performs a synchronous distributor API import for the given
// SKU, applies the enrichment result, and ensures the team has an inventory
// entry for the resulting part. It returns the part UUID on success.
//
// Sentinel errors (errDistributor*, errPartNotFoundAtDistributor) should be
// mapped to specific HTTP status codes by the caller; all other errors are
// internal failures.
func (h *Handler) importPartBySKU(ctx context.Context, teamID pgtype.UUID, distributorID string, sku string) (pgtype.UUID, error) {
	configs, err := h.Queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	var cfg *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if configs[i].DistributorID.String() == distributorID {
			cfg = &configs[i]
			break
		}
	}
	if cfg == nil {
		return pgtype.UUID{}, errDistributorNotConfigured
	}
	if cfg.ApiType == nil {
		return pgtype.UUID{}, errDistributorNoAPI
	}
	client := h.Distributors.Lookup(*cfg.ApiType)
	if client == nil {
		return pgtype.UUID{}, errDistributorNoClient
	}

	result, err := client.EnrichPart(ctx, sku, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("distributor API error: %w", err)
	}
	if result == nil {
		return pgtype.UUID{}, errPartNotFoundAtDistributor
	}

	partID, err := worker.ApplyEnrichmentResult(ctx, h.DB, h.Queries, teamID, cfg.DistributorID, result, nil, nil, h.Manufacturers)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("apply enrichment: %w", err)
	}

	// Ensure the team has an inventory entry for this part.
	_, err = h.Queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID,
		PartID: partID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = h.Queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
			TeamID: teamID,
			PartID: partID,
		})
	}

	return partID, nil
}

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

// maxSearchEnrichParts caps how many parts a single search-scoped re-enrichment may
// enqueue, so a broad search can't make thousands of live distributor API calls.
// Callers should narrow their search when this is exceeded.
const maxSearchEnrichParts = 1000

// RefreshSearchParameters handles POST /api/v1/teams/{teamId}/parts/refresh-parameters
// It re-enriches (one live distributor API call per part) every part that matches the
// same filter set as ListParts (q, mpn, category, package, lifecycle, scope, stock,
// storage, param_*) and has at least one API-integrated listing, scoping the operation
// to the user's current search. Returns 202 {"job_id","part_count"}, 204 if no parts
// are enrichable, or 400 if the match set exceeds the cap.
func (h *Handler) RefreshSearchParameters(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	f, matchedNone, ok := h.resolvePartFilters(w, r, teamID)
	if !ok {
		return
	}
	if matchedNone {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	rows, err := h.Queries.GetPartsForReenrichmentInSearch(r.Context(), dbgen.GetPartsForReenrichmentInSearchParams{
		TeamID:            f.TeamID,
		Q:                 f.Q,
		MpnExact:          f.MpnExact,
		CategoryID:        f.CategoryID,
		Package:           f.Package,
		LifecycleStatus:   f.LifecycleStatus,
		FilterScope:       f.FilterScope,
		StockStatus:       f.StockStatus,
		StorageLocationID: f.StorageLocationID,
		StorageCells:      f.StorageCells,
		PartIDFilter:      f.PartIDFilter,
	})
	if err != nil {
		slog.Error("GetPartsForReenrichmentInSearch failed", "team_id", teamID.String(), "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if len(rows) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(rows) > maxSearchEnrichParts {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("too many parts to enrich (%d, max %d) — narrow your search", len(rows), maxSearchEnrichParts),
			"TOO_MANY_PARTS")
		return
	}

	targets := make([]worker.EnrichTarget, len(rows))
	for i, row := range rows {
		targets[i] = worker.EnrichTarget{
			PartID:        row.PartID.String(),
			Sku:           row.Sku,
			DistributorID: row.DistributorID.String(),
		}
	}
	jobID := h.Worker.Enqueue(worker.JobPartEnrichBatch, teamID.String(), worker.PartEnrichBatchPayload{
		Targets: targets,
		UserID:  userIDFromContext(r),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "part_count": len(targets)})
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

	partID, err := h.importPartBySKU(r.Context(), teamID, req.DistributorID, req.SKU)
	if err != nil {
		h.writeImportError(w, "ImportPart", req.SKU, err)
		return
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"part_id": partID.String()})
}

// writeImportError maps importPartBySKU sentinel errors to appropriate HTTP
// responses and logs unexpected failures.
func (h *Handler) writeImportError(w http.ResponseWriter, op, sku string, err error) {
	switch {
	case errors.Is(err, errDistributorNotConfigured):
		writeError(w, http.StatusBadRequest, err.Error(), "BAD_REQUEST")
	case errors.Is(err, errDistributorNoAPI):
		writeError(w, http.StatusBadRequest, err.Error(), "BAD_REQUEST")
	case errors.Is(err, errDistributorNoClient):
		writeError(w, http.StatusInternalServerError, "no client registered for this distributor", "INTERNAL_ERROR")
	case errors.Is(err, errPartNotFoundAtDistributor):
		writeError(w, http.StatusNotFound, "part not found at distributor", "NOT_FOUND")
	default:
		slog.Error(op+": import failed", "sku", sku, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to import part: "+err.Error(), "INTERNAL_ERROR")
	}
}

// ListPartCategories handles GET /api/v1/teams/{teamId}/parts/categories
// Returns direct part counts per category present in the team's parts catalog.
func (h *Handler) ListPartCategories(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	rows, err := h.Queries.ListPartCategoryCounts(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	cats := make([]apitypes.CategoryPartCount, 0, len(rows))
	for _, row := range rows {
		if row.CategoryID != nil {
			cats = append(cats, apitypes.CategoryPartCount{ID: *row.CategoryID, Count: row.Count})
		}
	}
	writeJSON(w, http.StatusOK, apitypes.CategoryPartCounts{Categories: cats})
}

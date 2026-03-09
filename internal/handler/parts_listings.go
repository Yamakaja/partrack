package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AddListing handles POST /api/v1/teams/{teamId}/parts/{partId}/listings
// Attaches a distributor listing to a part and triggers enrichment. Returns 202 with job_id.
// Body: {sku, distributor_id} (both required).
func (h *Handler) AddListing(w http.ResponseWriter, r *http.Request) {
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

// DeleteListing handles DELETE /api/v1/teams/{teamId}/parts/{partId}/listings/{listingId}
// Removes a distributor listing and its distributor-sourced parameters. Returns 204.
func (h *Handler) DeleteListing(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}
	var listingID pgtype.UUID
	if err := listingID.Scan(chi.URLParam(r, "listingId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid listing ID", "BAD_REQUEST")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")

	listing, err := h.Queries.GetListingByPartAndID(r.Context(), dbgen.GetListingByPartAndIDParams{
		ID:     listingID,
		PartID: partID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "listing not found", "NOT_FOUND")
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
	if err := qtx.DeleteDistributorParameters(r.Context(), dbgen.DeleteDistributorParametersParams{
		PartID:              partID,
		SourceDistributorID: listing.DistributorID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := qtx.DeleteListing(r.Context(), listingID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "part_changed", teamIDStr, map[string]string{"part_id": partID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ListListings handles GET /api/v1/parts/{partId}/listings
func (h *Handler) ListListings(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	listings, err := h.Queries.ListListings(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID              string          `json:"id"`
		DistributorID   string          `json:"distributor_id"`
		DistributorName string          `json:"distributor_name"`
		Sku             string          `json:"sku"`
		PackagingType   string          `json:"packaging_type"`
		UnitPrice       *string         `json:"unit_price"`
		StockAvailable  *int32          `json:"stock_available"`
		Moq             int32           `json:"moq"`
		FixedOrderCost  *string         `json:"fixed_order_cost"`
		PriceBreaks     json.RawMessage `json:"price_breaks"`
		LastRefreshedAt *string         `json:"last_refreshed_at"`
	}
	items := make([]item, len(listings))
	for i, l := range listings {
		var lastRef *string
		if l.LastRefreshedAt.Valid {
			s := l.LastRefreshedAt.Time.UTC().Format(time.RFC3339)
			lastRef = &s
		}
		items[i] = item{
			ID:              l.ID.String(),
			DistributorID:   l.DistributorID.String(),
			DistributorName: l.DistributorName,
			Sku:             l.Sku,
			PackagingType:   l.PackagingType,
			UnitPrice:       numericString(l.UnitPrice),
			StockAvailable:  l.StockAvailable,
			Moq:             l.Moq,
			FixedOrderCost:  numericString(l.FixedOrderCost),
			PriceBreaks:     json.RawMessage(l.PriceBreaks),
			LastRefreshedAt: lastRef,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// RefreshListing handles POST /api/v1/teams/{teamId}/parts/{partId}/listings/{listingId}/refresh
// Triggers price/stock refresh for one listing. Returns 202 with job_id.
func (h *Handler) RefreshListing(w http.ResponseWriter, r *http.Request) {
	var listingID pgtype.UUID
	if err := listingID.Scan(chi.URLParam(r, "listingId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid listing ID", "BAD_REQUEST")
		return
	}

	listing, err := h.Queries.GetListing(r.Context(), listingID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "listing not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	teamID := chi.URLParam(r, "teamId")
	jobID := h.Worker.Enqueue(worker.JobListingRefresh, teamID, map[string]string{
		"listing_id":     listing.ID.String(),
		"part_id":        listing.PartID.String(),
		"distributor_id": listing.DistributorID.String(),
		"user_id":        userIDFromContext(r),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// RefreshAllListings handles POST /api/v1/teams/{teamId}/parts/{partId}/listings/refresh
// Enqueues a stock/price refresh for every listing on the part. Returns 202 with job IDs.
func (h *Handler) RefreshAllListings(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	listings, err := h.Queries.ListListings(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	teamID := chi.URLParam(r, "teamId")
	userID := userIDFromContext(r)
	jobIDs := make([]string, 0, len(listings))
	for _, l := range listings {
		jobID := h.Worker.Enqueue(worker.JobListingRefresh, teamID, map[string]string{
			"listing_id":     l.ID.String(),
			"part_id":        l.PartID.String(),
			"distributor_id": l.DistributorID.String(),
			"user_id":        userID,
		})
		jobIDs = append(jobIDs, jobID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_ids": jobIDs})
}

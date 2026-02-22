package handler

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// numericString formats a pgtype.Numeric as a fixed-point decimal string.
// Returns nil when the value is not valid (NULL).
func numericString(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	if n.NaN {
		s := "NaN"
		return &s
	}
	isNeg := n.Int.Sign() < 0
	absInt := new(big.Int).Abs(n.Int)
	intStr := absInt.String()

	var result string
	if n.Exp >= 0 {
		result = intStr + strings.Repeat("0", int(n.Exp))
	} else {
		exp := int(-n.Exp)
		if len(intStr) <= exp {
			result = "0." + strings.Repeat("0", exp-len(intStr)) + intStr
		} else {
			result = intStr[:len(intStr)-exp] + "." + intStr[len(intStr)-exp:]
		}
	}
	if isNeg {
		result = "-" + result
	}
	return &result
}

// parsePaginationParams parses limit and offset from query parameters.
// limit defaults to 50, max 200. offset defaults to 0.
func parsePaginationParams(r *http.Request) (limit, offset int32) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return
}

type partFullResponse struct {
	ID                         string  `json:"id"`
	Scope                      string  `json:"scope"`
	TeamID                     *string `json:"team_id"`
	Mpn                        *string `json:"mpn"`
	Manufacturer               *string `json:"manufacturer"`
	Description                *string `json:"description"`
	Category                   *string `json:"category"`
	Package                    *string `json:"package"`
	DatasheetURL               *string `json:"datasheet_url"`
	LifecycleStatus            string  `json:"lifecycle_status"`
	EnrichmentSourceID         *string `json:"enrichment_source_id"`
	EnrichedAt                 *string `json:"enriched_at"`
	PreferredParameterSourceID *string `json:"preferred_parameter_source_id"`
	CreatedAt                  string  `json:"created_at"`
}

func toPartFullResponse(p dbgen.Part) partFullResponse {
	resp := partFullResponse{
		ID:              p.ID.String(),
		Scope:           string(p.Scope),
		Mpn:             p.Mpn,
		Manufacturer:    p.Manufacturer,
		Description:     p.Description,
		Category:        p.Category,
		Package:         p.Package,
		DatasheetURL:    p.DatasheetUrl,
		LifecycleStatus: string(p.LifecycleStatus),
		CreatedAt:       p.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.TeamID.Valid {
		s := p.TeamID.String()
		resp.TeamID = &s
	}
	if p.EnrichmentSourceID.Valid {
		s := p.EnrichmentSourceID.String()
		resp.EnrichmentSourceID = &s
	}
	if p.EnrichedAt.Valid {
		s := p.EnrichedAt.Time.UTC().Format(time.RFC3339)
		resp.EnrichedAt = &s
	}
	if p.PreferredParameterSourceID.Valid {
		s := p.PreferredParameterSourceID.String()
		resp.PreferredParameterSourceID = &s
	}
	return resp
}

// ListParts handles GET /api/v1/parts
// Query: team_id?, q?, category?, package?, mpn?, lifecycle_status?, scope?, limit, offset
func (h *Handler) ListParts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := parsePaginationParams(r)

	params := dbgen.ListPartsParams{Limit: limit, Offset: offset}
	countParams := dbgen.CountPartsParams{}

	if v := q.Get("team_id"); v != "" {
		var teamID pgtype.UUID
		if err := teamID.Scan(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
			return
		}
		params.TeamID = teamID
		countParams.TeamID = teamID
	}
	if v := q.Get("q"); v != "" {
		params.Q = &v
		countParams.Q = &v
	}
	if v := q.Get("mpn"); v != "" {
		params.MpnExact = &v
		countParams.MpnExact = &v
	}
	if v := q.Get("category"); v != "" {
		params.Category = &v
		countParams.Category = &v
	}
	if v := q.Get("package"); v != "" {
		params.Package = &v
		countParams.Package = &v
	}
	if v := q.Get("lifecycle_status"); v != "" {
		params.LifecycleStatus = dbgen.NullPartLifecycleStatus{
			PartLifecycleStatus: dbgen.PartLifecycleStatus(v), Valid: true,
		}
		countParams.LifecycleStatus = params.LifecycleStatus
	}
	if v := q.Get("scope"); v != "" {
		params.FilterScope = dbgen.NullPartScope{PartScope: dbgen.PartScope(v), Valid: true}
		countParams.FilterScope = params.FilterScope
	}

	total, err := h.Queries.CountParts(r.Context(), countParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	parts, err := h.Queries.ListParts(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID              string  `json:"id"`
		Scope           string  `json:"scope"`
		Mpn             *string `json:"mpn"`
		Manufacturer    *string `json:"manufacturer"`
		Description     *string `json:"description"`
		Category        *string `json:"category"`
		Package         *string `json:"package"`
		LifecycleStatus string  `json:"lifecycle_status"`
	}
	items := make([]item, len(parts))
	for i, p := range parts {
		items[i] = item{
			ID:              p.ID.String(),
			Scope:           string(p.Scope),
			Mpn:             p.Mpn,
			Manufacturer:    p.Manufacturer,
			Description:     p.Description,
			Category:        p.Category,
			Package:         p.Package,
			LifecycleStatus: string(p.LifecycleStatus),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "limit": limit, "offset": offset, "items": items,
	})
}

// CreatePart handles POST /api/v1/parts
func (h *Handler) CreatePart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope           string  `json:"scope"`
		TeamID          *string `json:"team_id"`
		Mpn             *string `json:"mpn"`
		Manufacturer    *string `json:"manufacturer"`
		Description     *string `json:"description"`
		Category        *string `json:"category"`
		Package         *string `json:"package"`
		DatasheetURL    *string `json:"datasheet_url"`
		LifecycleStatus string  `json:"lifecycle_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Scope == "" {
		writeError(w, http.StatusBadRequest, "scope is required", "BAD_REQUEST")
		return
	}

	var teamID pgtype.UUID
	switch req.Scope {
	case "global":
		if req.TeamID != nil {
			writeError(w, http.StatusBadRequest, "team_id must not be set for global parts", "BAD_REQUEST")
			return
		}
	case "team":
		if req.TeamID == nil {
			writeError(w, http.StatusBadRequest, "team_id is required for team-scoped parts", "BAD_REQUEST")
			return
		}
		if err := teamID.Scan(*req.TeamID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "scope must be 'global' or 'team'", "BAD_REQUEST")
		return
	}

	lifecycleStatus := dbgen.PartLifecycleStatusActive
	if req.LifecycleStatus != "" {
		lifecycleStatus = dbgen.PartLifecycleStatus(req.LifecycleStatus)
	}

	part, err := h.Queries.CreatePart(r.Context(), dbgen.CreatePartParams{
		Scope:           dbgen.PartScope(req.Scope),
		TeamID:          teamID,
		Mpn:             req.Mpn,
		Manufacturer:    req.Manufacturer,
		Description:     req.Description,
		Category:        req.Category,
		Package:         req.Package,
		DatasheetUrl:    req.DatasheetURL,
		LifecycleStatus: lifecycleStatus,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	w.Header().Set("Location", "/api/v1/parts/"+part.ID.String())
	writeJSON(w, http.StatusCreated, toPartFullResponse(part))
}

// GetPart handles GET /api/v1/parts/{partId}
func (h *Handler) GetPart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	part, err := h.Queries.GetPart(r.Context(), partID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	params, err := h.Queries.ListParameters(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	listings, err := h.Queries.ListListings(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type userParam struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit"`
	}
	type distParam struct {
		ID                    string  `json:"id"`
		Name                  string  `json:"name"`
		Value                 string  `json:"value"`
		Unit                  *string `json:"unit"`
		SourceDistributorID   string  `json:"source_distributor_id"`
		SourceDistributorName *string `json:"source_distributor_name"`
	}
	userParams := []userParam{}
	distParams := []distParam{}
	for _, p := range params {
		if p.SourceType == dbgen.ParameterSourceTypeUser {
			userParams = append(userParams, userParam{
				ID: p.ID.String(), Name: p.Name, Value: p.Value, Unit: p.Unit,
			})
		} else {
			distParams = append(distParams, distParam{
				ID:                    p.ID.String(),
				Name:                  p.Name,
				Value:                 p.Value,
				Unit:                  p.Unit,
				SourceDistributorID:   p.SourceDistributorID.String(),
				SourceDistributorName: p.SourceDistributorName,
			})
		}
	}

	type listingItem struct {
		ID              string  `json:"id"`
		DistributorID   string  `json:"distributor_id"`
		DistributorName string  `json:"distributor_name"`
		Sku             string  `json:"sku"`
		PackagingType   string  `json:"packaging_type"`
		UnitPrice       *string `json:"unit_price"`
		StockAvailable  *int32  `json:"stock_available"`
		Moq             int32   `json:"moq"`
		FixedOrderCost  *string `json:"fixed_order_cost"`
		LastRefreshedAt *string `json:"last_refreshed_at"`
	}
	listingItems := make([]listingItem, len(listings))
	for i, l := range listings {
		var lastRef *string
		if l.LastRefreshedAt.Valid {
			s := l.LastRefreshedAt.Time.UTC().Format(time.RFC3339)
			lastRef = &s
		}
		listingItems[i] = listingItem{
			ID:              l.ID.String(),
			DistributorID:   l.DistributorID.String(),
			DistributorName: l.DistributorName,
			Sku:             l.Sku,
			PackagingType:   l.PackagingType,
			UnitPrice:       numericString(l.UnitPrice),
			StockAvailable:  l.StockAvailable,
			Moq:             l.Moq,
			FixedOrderCost:  numericString(l.FixedOrderCost),
			LastRefreshedAt: lastRef,
		}
	}

	base := toPartFullResponse(part)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                            base.ID,
		"scope":                         base.Scope,
		"team_id":                       base.TeamID,
		"mpn":                           base.Mpn,
		"manufacturer":                  base.Manufacturer,
		"description":                   base.Description,
		"category":                      base.Category,
		"package":                       base.Package,
		"datasheet_url":                 base.DatasheetURL,
		"lifecycle_status":              base.LifecycleStatus,
		"enrichment_source_id":          base.EnrichmentSourceID,
		"enriched_at":                   base.EnrichedAt,
		"preferred_parameter_source_id": base.PreferredParameterSourceID,
		"created_at":                    base.CreatedAt,
		"parameters": map[string]any{
			"user_specified":      userParams,
			"distributor_sourced": distParams,
		},
		"listings": listingItems,
	})
}

// UpdatePart handles PATCH /api/v1/parts/{partId}
func (h *Handler) UpdatePart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Description                *string `json:"description"`
		Category                   *string `json:"category"`
		Package                    *string `json:"package"`
		DatasheetURL               *string `json:"datasheet_url"`
		LifecycleStatus            *string `json:"lifecycle_status"`
		PreferredParameterSourceID *string `json:"preferred_parameter_source_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	params := dbgen.UpdatePartParams{ID: partID}
	params.Description = req.Description
	params.Category = req.Category
	params.Package = req.Package
	params.DatasheetUrl = req.DatasheetURL
	if req.LifecycleStatus != nil {
		params.LifecycleStatus = dbgen.NullPartLifecycleStatus{
			PartLifecycleStatus: dbgen.PartLifecycleStatus(*req.LifecycleStatus),
			Valid:               true,
		}
	}
	if req.PreferredParameterSourceID != nil {
		if err := params.PreferredParameterSourceID.Scan(*req.PreferredParameterSourceID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid preferred_parameter_source_id", "BAD_REQUEST")
			return
		}
	}

	part, err := h.Queries.UpdatePart(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, toPartFullResponse(part))
}

// EnrichPart handles POST /api/v1/parts/{partId}/enrich
// Triggers re-enrichment from distributor APIs. Returns 202 with job_id.
// Query: team_id (required) — the team whose distributor configs to use; caller must be a member.
func (h *Handler) EnrichPart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	teamIDStr := r.URL.Query().Get("team_id")
	if teamIDStr == "" {
		writeError(w, http.StatusBadRequest, "team_id query parameter is required", "BAD_REQUEST")
		return
	}
	var teamID pgtype.UUID
	if err := teamID.Scan(teamIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
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
	if _, err := h.Queries.CheckTeamMembership(r.Context(), dbgen.CheckTeamMembershipParams{
		TeamID: teamID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
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

	jobID := h.Worker.Enqueue(worker.JobPartEnrich, teamIDStr, map[string]string{
		"part_id": partID.String(),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// ListParameters handles GET /api/v1/parts/{partId}/parameters
func (h *Handler) ListParameters(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	params, err := h.Queries.ListParameters(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type userParam struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit"`
	}
	type distParam struct {
		ID                    string  `json:"id"`
		Name                  string  `json:"name"`
		Value                 string  `json:"value"`
		Unit                  *string `json:"unit"`
		SourceDistributorID   string  `json:"source_distributor_id"`
		SourceDistributorName *string `json:"source_distributor_name"`
	}
	userParams := []userParam{}
	distParams := []distParam{}
	for _, p := range params {
		if p.SourceType == dbgen.ParameterSourceTypeUser {
			userParams = append(userParams, userParam{
				ID: p.ID.String(), Name: p.Name, Value: p.Value, Unit: p.Unit,
			})
		} else {
			distParams = append(distParams, distParam{
				ID:                    p.ID.String(),
				Name:                  p.Name,
				Value:                 p.Value,
				Unit:                  p.Unit,
				SourceDistributorID:   p.SourceDistributorID.String(),
				SourceDistributorName: p.SourceDistributorName,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_specified":      userParams,
		"distributor_sourced": distParams,
	})
}

// CreateParameter handles POST /api/v1/parts/{partId}/parameters
// Body: {name, value, unit?}
func (h *Handler) CreateParameter(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "name and value are required", "BAD_REQUEST")
		return
	}

	param, err := h.Queries.UpsertUserParameter(r.Context(), dbgen.UpsertUserParameterParams{
		PartID: partID,
		Name:   req.Name,
		Value:  req.Value,
		Unit:   req.Unit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          param.ID.String(),
		"name":        param.Name,
		"value":       param.Value,
		"unit":        param.Unit,
		"source_type": string(param.SourceType),
	})
}

// DeleteParameter handles DELETE /api/v1/parts/{partId}/parameters/{parameterId}
func (h *Handler) DeleteParameter(w http.ResponseWriter, r *http.Request) {
	var paramID pgtype.UUID
	if err := paramID.Scan(chi.URLParam(r, "parameterId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid parameter ID", "BAD_REQUEST")
		return
	}

	if err := h.Queries.DeleteParameter(r.Context(), paramID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
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
		ID              string  `json:"id"`
		DistributorID   string  `json:"distributor_id"`
		DistributorName string  `json:"distributor_name"`
		Sku             string  `json:"sku"`
		PackagingType   string  `json:"packaging_type"`
		UnitPrice       *string `json:"unit_price"`
		StockAvailable  *int32  `json:"stock_available"`
		Moq             int32   `json:"moq"`
		FixedOrderCost  *string `json:"fixed_order_cost"`
		LastRefreshedAt *string `json:"last_refreshed_at"`
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
			LastRefreshedAt: lastRef,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// RefreshListing handles POST /api/v1/parts/{partId}/listings/{listingId}/refresh
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

	teamID := r.URL.Query().Get("team_id") // optional; used for SSE routing only
	jobID := h.Worker.Enqueue(worker.JobListingRefresh, teamID, map[string]string{
		"listing_id":     listing.ID.String(),
		"part_id":        listing.PartID.String(),
		"distributor_id": listing.DistributorID.String(),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

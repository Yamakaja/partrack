package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
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

// GetDistributorCredentialSpec handles GET /api/v1/distributors/{distributorId}/credential-spec
// Returns the list of credential fields the distributor's Client requires.
// Returns an empty array when the distributor has no API integration or no registered client.
func (h *Handler) GetDistributorCredentialSpec(w http.ResponseWriter, r *http.Request) {
	var id pgtype.UUID
	if err := id.Scan(chi.URLParam(r, "distributorId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid distributor ID", "BAD_REQUEST")
		return
	}

	dist, err := h.Queries.GetDistributor(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "distributor not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if dist.ApiType == nil {
		writeJSON(w, http.StatusOK, []distributor.CredentialField{})
		return
	}

	client := h.Distributors.Lookup(*dist.ApiType)
	if client == nil {
		writeJSON(w, http.StatusOK, []distributor.CredentialField{})
		return
	}

	spec := client.CredentialSpec()
	if spec == nil {
		spec = []distributor.CredentialField{}
	}
	writeJSON(w, http.StatusOK, spec)
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
		DistributorID        string                      `json:"distributor_id"`
		DistributorName      string                      `json:"distributor_name"`
		Enabled              bool                        `json:"enabled"`
		Preferred            bool                        `json:"preferred"`
		CacheDurationSeconds int32                       `json:"cache_duration_seconds"`
		HasCredentials       bool                        `json:"has_credentials"`
		ApiCredentials       map[string]any              `json:"api_credentials,omitempty"`
		RateLimit            *distributor.RateLimitStatus `json:"rate_limit,omitempty"`
	}
	items := make([]item, len(configs))
	for i, c := range configs {
		hasCreds, _ := c.HasCredentials.(bool)
		it := item{
			DistributorID:        c.DistributorID.String(),
			DistributorName:      c.DistributorName,
			Enabled:              c.Enabled,
			Preferred:            c.Preferred,
			CacheDurationSeconds: c.CacheDurationSeconds,
			HasCredentials:       hasCreds,
		}
		// Include non-secret credential fields so the frontend can pre-populate the form.
		if len(c.ApiCredentials) > 0 && c.ApiType != nil {
			if client := h.Distributors.Lookup(*c.ApiType); client != nil {
				secretKeys := make(map[string]bool)
				for _, f := range client.CredentialSpec() {
					if f.Secret {
						secretKeys[f.Key] = true
					}
				}
				var creds map[string]any
				if err := json.Unmarshal(c.ApiCredentials, &creds); err == nil {
					for k := range secretKeys {
						delete(creds, k)
					}
					if len(creds) > 0 {
						it.ApiCredentials = creds
					}
				}
				// Include rate limit status if the client supports it.
				if rl, ok := client.(distributor.RateLimitReporter); ok {
					it.RateLimit = rl.RateLimitStatus(c.ApiCredentials)
				}
			}
		}
		items[i] = it
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

	// api_credentials key was present — merge submitted values with existing,
	// preserving existing values for secret fields that were left blank.
	var apiCredentials []byte
	if string(raw["api_credentials"]) != "null" {
		submitted := raw["api_credentials"]

		// Fetch existing credentials + api_type to enable field-level merging.
		existing, rawErr := h.Queries.GetDistributorConfigRaw(r.Context(), dbgen.GetDistributorConfigRawParams{
			TeamID:        teamID,
			DistributorID: distributorID,
		})
		if rawErr != nil && !errors.Is(rawErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		// Unmarshal both submitted and existing credentials.
		var submittedMap map[string]any
		if err := json.Unmarshal(submitted, &submittedMap); err != nil {
			writeError(w, http.StatusBadRequest, "invalid api_credentials", "BAD_REQUEST")
			return
		}

		// Merge: for secret fields with an empty/absent submitted value, keep the existing value.
		if !errors.Is(rawErr, pgx.ErrNoRows) && len(existing.ApiCredentials) > 0 && existing.ApiType != nil {
			if client := h.Distributors.Lookup(*existing.ApiType); client != nil {
				var existingMap map[string]any
				if err := json.Unmarshal(existing.ApiCredentials, &existingMap); err == nil {
					for _, f := range client.CredentialSpec() {
						if !f.Secret {
							continue
						}
						submitted, hasKey := submittedMap[f.Key]
						isEmpty := !hasKey || submitted == "" || submitted == nil
						if isEmpty {
							if existingVal, ok := existingMap[f.Key]; ok {
								submittedMap[f.Key] = existingVal
							} else {
								delete(submittedMap, f.Key)
							}
						}
					}
				}
			}
		}

		merged, err := json.Marshal(submittedMap)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		apiCredentials = merged
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

// SearchDistributor handles GET /api/v1/teams/{teamId}/distributor-configs/{distributorId}/search
// Query params: keywords (required), limit (default 20, max 50), offset (default 0).
func (h *Handler) SearchDistributor(w http.ResponseWriter, r *http.Request) {
	var teamID, distributorID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := distributorID.Scan(chi.URLParam(r, "distributorId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid distributor ID", "BAD_REQUEST")
		return
	}

	keywords := r.URL.Query().Get("keywords")
	if keywords == "" {
		writeError(w, http.StatusBadRequest, "keywords query parameter is required", "BAD_REQUEST")
		return
	}

	limit := int32(20)
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 32); err == nil && v > 0 {
			limit = int32(v)
		}
	}
	if limit > 50 {
		limit = 50
	}

	offset := int32(0)
	if s := r.URL.Query().Get("offset"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 32); err == nil && v >= 0 {
			offset = int32(v)
		}
	}

	cfg, err := h.Queries.GetDistributorConfigForSearch(r.Context(), dbgen.GetDistributorConfigForSearchParams{
		TeamID:        teamID,
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
		writeError(w, http.StatusUnprocessableEntity, "no client registered for distributor api_type", "NO_CLIENT")
		return
	}

	result, err := client.SearchParts(r.Context(), distributor.SearchQuery{
		Keywords: keywords,
		Limit:    limit,
		Offset:   offset,
	}, cfg.ApiCredentials)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error(), "SEARCH_ERROR")
		return
	}

	type productResp struct {
		MPN            string  `json:"mpn"`
		Manufacturer   string  `json:"manufacturer"`
		Description    string  `json:"description"`
		Category       string  `json:"category"`
		PrimarySKU     string  `json:"primary_sku"`
		Package        string  `json:"package"`
		UnitPrice      string  `json:"unit_price"`
		StockAvailable *int32  `json:"stock_available"`
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

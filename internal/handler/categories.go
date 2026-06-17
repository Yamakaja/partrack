package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
)

// ListCategories handles GET /api/v1/categories
// Returns the full category registry as a flat list. No auth required.
// The frontend builds the tree from parent_id references.
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Queries.ListCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type categoryItem struct {
		ID            int32    `json:"id"`
		Slug          string   `json:"slug"`
		DisplayName   string   `json:"display_name"`
		ParentID      *int32   `json:"parent_id"`
		SortOrder     int32    `json:"sort_order"`
		ValueParam    *string  `json:"value_param"`
		PrimaryParams []string `json:"primary_params"`
	}

	items := make([]categoryItem, len(rows))
	for i, c := range rows {
		var params []string
		if err := json.Unmarshal(c.PrimaryParams, &params); err != nil {
			params = []string{}
		}
		items[i] = categoryItem{
			ID:            c.ID,
			Slug:          c.Slug,
			DisplayName:   c.DisplayName,
			ParentID:      c.ParentID,
			SortOrder:     c.SortOrder,
			ValueParam:    c.ValueParam,
			PrimaryParams: params,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"categories": items})
}

// AdminUpdateCategoryParams handles PATCH /api/v1/admin/categories/{id}/params
// Body: {value_param: string|null, primary_params: string[]}. Curates the
// category's class profile (the magnitude slug + the ordered list of relevant
// parameter slugs). Every referenced slug must exist in the parameter registry.
func (h *Handler) AdminUpdateCategoryParams(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || n <= 0 {
		writeError(w, http.StatusBadRequest, "invalid category ID", "BAD_REQUEST")
		return
	}

	var req struct {
		ValueParam    *string  `json:"value_param"`
		PrimaryParams []string `json:"primary_params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if req.PrimaryParams == nil {
		req.PrimaryParams = []string{}
	}

	// Validate every referenced slug against the parameter registry.
	defs, err := h.Queries.ListParameterDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	known := make(map[string]bool, len(defs))
	for _, d := range defs {
		known[d.Slug] = true
	}
	for _, slug := range req.PrimaryParams {
		if !known[slug] {
			writeError(w, http.StatusBadRequest, "unknown parameter slug: "+slug, "BAD_REQUEST")
			return
		}
	}
	// Normalise value_param: empty string means "not parametric" (NULL).
	var valueParam *string
	if req.ValueParam != nil && *req.ValueParam != "" {
		if !known[*req.ValueParam] {
			writeError(w, http.StatusBadRequest, "unknown value_param slug: "+*req.ValueParam, "BAD_REQUEST")
			return
		}
		valueParam = req.ValueParam
	}

	primaryParamsJSON, err := json.Marshal(req.PrimaryParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	row, err := h.Queries.UpdateCategoryParams(r.Context(), dbgen.UpdateCategoryParamsParams{
		ID:            int32(n),
		ValueParam:    valueParam,
		PrimaryParams: primaryParamsJSON,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var params []string
	if err := json.Unmarshal(row.PrimaryParams, &params); err != nil {
		params = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             row.ID,
		"slug":           row.Slug,
		"display_name":   row.DisplayName,
		"parent_id":      row.ParentID,
		"sort_order":     row.SortOrder,
		"value_param":    row.ValueParam,
		"primary_params": params,
	})
}

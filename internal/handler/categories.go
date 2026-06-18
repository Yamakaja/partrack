package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// CategoryParameterSchema handles
// GET /api/v1/teams/{teamId}/categories/{categoryId}/parameter-schema
//
// Returns the category's primary parameters (the curated, interpretation-relevant
// set) with display metadata and catalog-derived enum options, so the BOM
// interpretation review UI can render labelled, value-type-aware inputs and an
// add-parameter picker without ever exposing raw slugs. team_id scopes the enum
// vocabulary to the team's catalog (global + team parts).
func (h *Handler) CategoryParameterSchema(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	catID, err := strconv.Atoi(chi.URLParam(r, "categoryId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category id", "BAD_REQUEST")
		return
	}

	cat, err := h.Queries.GetCategory(r.Context(), int32(catID))
	if err != nil {
		writeError(w, http.StatusNotFound, "category not found", "NOT_FOUND")
		return
	}
	var primary []string
	_ = json.Unmarshal(cat.PrimaryParams, &primary)

	defs, err := h.Queries.ListParameterDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defBySlug := make(map[string]dbgen.ParameterDefinition, len(defs))
	for _, d := range defs {
		defBySlug[d.Slug] = d
	}

	// Catalog-derived value vocabulary (enum/text distinct values + packages in use).
	optsBySlug := map[string][]string{}
	if rows, err := h.Queries.GetCategoryParameters(r.Context(), dbgen.GetCategoryParametersParams{ID: int32(catID), TeamID: teamID}); err == nil {
		for _, row := range rows {
			if vals := toStringSlice(row.EnumValues); len(vals) > 0 {
				optsBySlug[row.Slug] = vals
			}
		}
	}
	if pkgs, err := h.Queries.ListCategoryPackages(r.Context(), dbgen.ListCategoryPackagesParams{CategoryID: int32(catID), TeamID: teamID}); err == nil {
		var list []string
		for _, p := range pkgs {
			if p != nil {
				list = append(list, *p)
			}
		}
		if len(list) > 0 {
			optsBySlug["package"] = list
		}
	}

	type paramSchema struct {
		Slug        string   `json:"slug"`
		DisplayName string   `json:"display_name"`
		UnitSymbol  string   `json:"unit_symbol"`
		ValueType   string   `json:"value_type"`
		Options     []string `json:"options"`
	}
	out := make([]paramSchema, 0, len(primary))
	for _, slug := range primary {
		ps := paramSchema{Slug: slug, Options: optsBySlug[slug]}
		if ps.Options == nil {
			ps.Options = []string{}
		}
		if def, ok := defBySlug[slug]; ok {
			ps.DisplayName = def.DisplayName
			ps.ValueType = def.ValueType
			if def.UnitSymbol != nil {
				ps.UnitSymbol = *def.UnitSymbol
			}
		} else {
			ps.DisplayName = slug
			ps.ValueType = "text"
		}
		out = append(out, ps)
	}

	valueParam := ""
	if cat.ValueParam != nil {
		valueParam = *cat.ValueParam
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"category_id": cat.ID,
		"slug":        cat.Slug,
		"value_param": valueParam,
		"params":      out,
	})
}

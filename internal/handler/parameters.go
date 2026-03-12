package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetCategoryParameters handles GET /api/v1/categories/{categoryId}/parameters
// Returns the parameter definitions present in a category (and its descendants),
// with value ranges for numeric params and distinct values for enum/text params.
// Optional ?team_id=... includes team-scoped parts in addition to global parts.
func (h *Handler) GetCategoryParameters(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "categoryId"))
	if err != nil || n <= 0 {
		writeError(w, http.StatusBadRequest, "invalid category ID", "BAD_REQUEST")
		return
	}

	arg := dbgen.GetCategoryParametersParams{ID: int32(n)}
	if v := r.URL.Query().Get("team_id"); v != "" {
		if err := arg.TeamID.Scan(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
			return
		}

		user := middleware.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "authentication required", "UNAUTHORIZED")
			return
		}
		if !user.IsAdmin {
			var userID pgtype.UUID
			if err := userID.Scan(user.ID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			m, err := h.checkTeamMembership(r.Context(), arg.TeamID, userID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if m == nil {
				writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
				return
			}
		}
	}

	rows, err := h.Queries.GetCategoryParameters(r.Context(), arg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type catParam struct {
		ID          int32    `json:"id"`
		Slug        string   `json:"slug"`
		DisplayName string   `json:"display_name"`
		Unit        *string  `json:"unit"`
		UnitSymbol  *string  `json:"unit_symbol"`
		ValueType   string   `json:"value_type"`
		MinNumeric  *float64 `json:"min_numeric"`
		MaxNumeric  *float64 `json:"max_numeric"`
		EnumValues  []string `json:"enum_values"`
	}

	out := make([]catParam, 0, len(rows))
	for _, row := range rows {
		p := catParam{
			ID:          row.ID,
			Slug:        row.Slug,
			DisplayName: row.DisplayName,
			Unit:        row.Unit,
			UnitSymbol:  row.UnitSymbol,
			ValueType:   row.ValueType,
		}
		if f, ok := row.MinNumeric.(float64); ok {
			p.MinNumeric = &f
		}
		if f, ok := row.MaxNumeric.(float64); ok {
			p.MaxNumeric = &f
		}
		p.EnumValues = toStringSlice(row.EnumValues)
		if p.EnumValues == nil {
			p.EnumValues = []string{}
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// toStringSlice converts a pgx-scanned array interface{} value to []string.
// pgx v5 returns text[] as pgtype.Array[string] when scanned into interface{}.
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case pgtype.Array[string]:
		if !s.Valid {
			return nil
		}
		return s.Elements
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// ListParameterDefinitions handles GET /api/v1/parameter-definitions
// Returns all canonical parameter definitions. Public, no auth required.
func (h *Handler) ListParameterDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := h.Queries.ListParameterDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type paramDef struct {
		ID          int32   `json:"id"`
		Slug        string  `json:"slug"`
		DisplayName string  `json:"display_name"`
		Unit        *string `json:"unit"`
		UnitSymbol  *string `json:"unit_symbol"`
		ValueType   string  `json:"value_type"`
	}
	out := make([]paramDef, len(defs))
	for i, d := range defs {
		out[i] = paramDef{
			ID:          int32(d.ID),
			Slug:        d.Slug,
			DisplayName: d.DisplayName,
			Unit:        d.Unit,
			UnitSymbol:  d.UnitSymbol,
			ValueType:   d.ValueType,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminListParameterDefinitions handles GET /api/v1/admin/parameter-definitions
func (h *Handler) AdminListParameterDefinitions(w http.ResponseWriter, r *http.Request) {
	h.ListParameterDefinitions(w, r)
}

// AdminCreateParameterDefinition handles POST /api/v1/admin/parameter-definitions
// Body: {slug, display_name, unit?, unit_symbol?, value_type}
func (h *Handler) AdminCreateParameterDefinition(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug        string  `json:"slug"`
		DisplayName string  `json:"display_name"`
		Unit        *string `json:"unit"`
		UnitSymbol  *string `json:"unit_symbol"`
		ValueType   string  `json:"value_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Slug == "" || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "slug and display_name are required", "BAD_REQUEST")
		return
	}
	if req.ValueType == "" {
		req.ValueType = "numeric"
	}

	def, err := h.Queries.CreateParameterDefinition(r.Context(), dbgen.CreateParameterDefinitionParams{
		Slug:        req.Slug,
		DisplayName: req.DisplayName,
		Unit:        req.Unit,
		UnitSymbol:  req.UnitSymbol,
		ValueType:   req.ValueType,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           int32(def.ID),
		"slug":         def.Slug,
		"display_name": def.DisplayName,
		"unit":         def.Unit,
		"unit_symbol":  def.UnitSymbol,
		"value_type":   def.ValueType,
	})
}

// AdminUpdateParameterDefinition handles PATCH /api/v1/admin/parameter-definitions/{id}
// Body: {slug?, display_name?, unit?, unit_symbol?, value_type?}
func (h *Handler) AdminUpdateParameterDefinition(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || n <= 0 {
		writeError(w, http.StatusBadRequest, "invalid parameter definition ID", "BAD_REQUEST")
		return
	}
	paramDefID := int32(n)

	existing, err := h.Queries.GetParameterDefinition(r.Context(), int32(paramDefID))
	if err != nil {
		writeError(w, http.StatusNotFound, "parameter definition not found", "NOT_FOUND")
		return
	}

	var req struct {
		Slug        *string `json:"slug"`
		DisplayName *string `json:"display_name"`
		Unit        *string `json:"unit"`
		UnitSymbol  *string `json:"unit_symbol"`
		ValueType   *string `json:"value_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	// Apply patch fields
	if req.Slug != nil {
		existing.Slug = *req.Slug
	}
	if req.DisplayName != nil {
		existing.DisplayName = *req.DisplayName
	}
	if req.Unit != nil {
		existing.Unit = req.Unit
	}
	if req.UnitSymbol != nil {
		existing.UnitSymbol = req.UnitSymbol
	}
	if req.ValueType != nil {
		existing.ValueType = *req.ValueType
	}

	updated, err := h.Queries.UpdateParameterDefinition(r.Context(), dbgen.UpdateParameterDefinitionParams{
		ID:          existing.ID,
		Slug:        existing.Slug,
		DisplayName: existing.DisplayName,
		Unit:        existing.Unit,
		UnitSymbol:  existing.UnitSymbol,
		ValueType:   existing.ValueType,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           int32(updated.ID),
		"slug":         updated.Slug,
		"display_name": updated.DisplayName,
		"unit":         updated.Unit,
		"unit_symbol":  updated.UnitSymbol,
		"value_type":   updated.ValueType,
	})
}

// AdminListDistributorParamMappings handles GET /api/v1/admin/distributor-parameter-mappings
// Query params: distributor_id (optional), unmapped=true (optional)
func (h *Handler) AdminListDistributorParamMappings(w http.ResponseWriter, r *http.Request) {
	var distID pgtype.UUID
	if s := r.URL.Query().Get("distributor_id"); s != "" {
		if err := distID.Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid distributor_id", "BAD_REQUEST")
			return
		}
	}
	unmappedOnly := r.URL.Query().Get("unmapped") == "true"

	rows, err := h.Queries.ListDistributorParamMappings(r.Context(), dbgen.ListDistributorParamMappingsParams{
		DistributorID: distID,
		UnmappedOnly:  unmappedOnly,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type mapping struct {
		ID                   string   `json:"id"`
		DistributorID        string   `json:"distributor_id"`
		DistributorName      string   `json:"distributor_name"`
		DistributorParamID   string   `json:"distributor_param_id"`
		DistributorParamName string   `json:"distributor_param_name"`
		ParameterID          *int32   `json:"parameter_id"`
		Ignored              bool     `json:"ignored"`
		PartCount            int64    `json:"part_count"`
		SampleValues         []string `json:"sample_values"`
	}
	out := make([]mapping, len(rows))
	for i, row := range rows {
		samples := toStringSlice(row.SampleValues)
		if samples == nil {
			samples = []string{}
		}
		out[i] = mapping{
			ID:                   row.ID.String(),
			DistributorID:        row.DistributorID.String(),
			DistributorName:      row.DistributorName,
			DistributorParamID:   row.DistributorParamID,
			DistributorParamName: row.DistributorParamName,
			ParameterID:          row.ParameterID,
			Ignored:              row.Ignored,
			PartCount:            row.PartCount,
			SampleValues:         samples,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminUpdateDistributorParamMapping handles PATCH /api/v1/admin/distributor-parameter-mappings/{id}
// Body: {parameter_id?: int|null, ignored?: bool}
func (h *Handler) AdminUpdateDistributorParamMapping(w http.ResponseWriter, r *http.Request) {
	var mappingID pgtype.UUID
	if err := mappingID.Scan(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid mapping ID", "BAD_REQUEST")
		return
	}

	var req struct {
		ParameterID *int32 `json:"parameter_id"`
		Ignored     *bool  `json:"ignored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if req.ParameterID != nil {
		if err := h.Queries.SetMappingParameterID(r.Context(), dbgen.SetMappingParameterIDParams{
			ID:          mappingID,
			ParameterID: req.ParameterID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}
	if req.Ignored != nil {
		if err := h.Queries.SetMappingIgnored(r.Context(), dbgen.SetMappingIgnoredParams{
			ID:      mappingID,
			Ignored: *req.Ignored,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

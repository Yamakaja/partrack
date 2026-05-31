package handler

import (
	"encoding/json"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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

	type paramBase struct {
		ID                   string   `json:"id"`
		Name                 string   `json:"name"`
		Value                string   `json:"value"`
		Unit                 *string  `json:"unit"`
		ParameterID          *int32   `json:"parameter_id"`
		ParameterSlug        *string  `json:"parameter_slug"`
		ParameterDisplayName *string  `json:"parameter_display_name"`
		ParameterUnitSymbol  *string  `json:"parameter_unit_symbol"`
		NumericValue         *float64 `json:"numeric_value"`
		NumericValueMax      *float64 `json:"numeric_value_max"`
	}
	type userParam struct {
		paramBase
	}
	type distParam struct {
		paramBase
		SourceDistributorID   string  `json:"source_distributor_id"`
		SourceDistributorName *string `json:"source_distributor_name"`
	}
	userParams := []userParam{}
	distParams := []distParam{}
	for _, p := range params {
		base := paramBase{
			ID:                   p.ID.String(),
			Name:                 p.Name,
			Value:                p.Value,
			Unit:                 p.Unit,
			ParameterID:          p.ParameterID,
			ParameterSlug:        p.ParameterSlug,
			ParameterDisplayName: p.ParameterDisplayName,
			ParameterUnitSymbol:  p.ParameterUnitSymbol,
			NumericValue:         p.NumericValue,
			NumericValueMax:      p.NumericValueMax,
		}
		if p.SourceType == dbgen.ParameterSourceTypeUser {
			userParams = append(userParams, userParam{base})
		} else {
			distParams = append(distParams, distParam{
				paramBase:             base,
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

	if _, ok := h.requirePartManualEdit(w, r, partID); !ok {
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
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}
	var paramID pgtype.UUID
	if err := paramID.Scan(chi.URLParam(r, "parameterId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid parameter ID", "BAD_REQUEST")
		return
	}
	if _, ok := h.requirePartManualEdit(w, r, partID); !ok {
		return
	}

	if err := h.Queries.DeleteParameter(r.Context(), dbgen.DeleteParameterParams{
		ID:     paramID,
		PartID: partID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateBOMItem handles PATCH /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}
// Body: {resolved_part_id?} — manually resolve or re-resolve a BOM line item (pass null to clear).
//
//	{ignored?} — toggle ignore state on the item.
func (h *Handler) UpdateBOMItem(w http.ResponseWriter, r *http.Request) {
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}

	var body struct {
		ResolvedPartID *string `json:"resolved_part_id"`
		Ignored        *bool   `json:"ignored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	strPtr := func(s string) *string { return &s }

	// Ignore/un-ignore path.
	if body.Ignored != nil {
		row, err := h.Queries.IgnoreBOMLineItem(r.Context(), dbgen.IgnoreBOMLineItemParams{
			Ignored:       *body.Ignored,
			ID:            itemID,
			BomRevisionID: revID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                    row.ID.String(),
			"reference_designators": row.ReferenceDesignators,
			"quantity":              row.Quantity,
			"raw_description":       row.RawDescription,
			"resolved_part_id":      uuidPtrString(row.ResolvedPartID),
			"resolution_state":      row.ResolutionState,
			"ignored":               row.Ignored,
		})
		return
	}

	// Resolve/clear path.
	var resolvedPartID pgtype.UUID
	var resolutionState *string
	if body.ResolvedPartID != nil && *body.ResolvedPartID != "" {
		if err := resolvedPartID.Scan(*body.ResolvedPartID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid resolved_part_id", "BAD_REQUEST")
			return
		}
		resolutionState = strPtr("manual")
	}
	// If body.ResolvedPartID is nil or empty, resolvedPartID stays zero (NULL) and
	// resolutionState stays nil (NULL), which clears any prior resolution.

	row, err := h.Queries.UpdateBOMLineItem(r.Context(), dbgen.UpdateBOMLineItemParams{
		ResolvedPartID:  resolvedPartID,
		ResolutionState: resolutionState,
		ID:              itemID,
		BomRevisionID:   revID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                    row.ID.String(),
		"reference_designators": row.ReferenceDesignators,
		"quantity":              row.Quantity,
		"raw_description":       row.RawDescription,
		"resolved_part_id":      uuidPtrString(row.ResolvedPartID),
		"resolution_state":      row.ResolutionState,
		"ignored":               false,
	})
}

// AddSubstitute handles POST /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes
// Body: {part_id}
func (h *Handler) AddSubstitute(w http.ResponseWriter, r *http.Request) {
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}

	var body struct {
		PartID string `json:"part_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.PartID == "" {
		writeError(w, http.StatusBadRequest, "part_id is required", "BAD_REQUEST")
		return
	}

	var partID pgtype.UUID
	if err := partID.Scan(body.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}

	err := h.Queries.AddBOMSubstitute(r.Context(), dbgen.AddBOMSubstituteParams{
		BomLineItemID: itemID,
		PartID:        partID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "substitute already exists", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"bom_line_item_id": itemID.String(),
		"part_id":          partID.String(),
	})
}

// ListSubstitutes handles GET /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/substitutes
// Returns all substitutes for all line items in the revision as a flat list.
func (h *Handler) ListSubstitutes(w http.ResponseWriter, r *http.Request) {
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.ListAllSubstitutesForRevision(r.Context(), revID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type subItem struct {
		BOMLineItemID string `json:"bom_line_item_id"`
		PartID        string `json:"part_id"`
		PartMPN       string `json:"part_mpn"`
	}
	items := make([]subItem, len(rows))
	for i, row := range rows {
		mpn := ""
		if row.PartMpn != nil {
			mpn = *row.PartMpn
		}
		items[i] = subItem{
			BOMLineItemID: row.BomLineItemID.String(),
			PartID:        row.PartID.String(),
			PartMPN:       mpn,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// RemoveSubstitute handles DELETE /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/{partId}
func (h *Handler) RemoveSubstitute(w http.ResponseWriter, r *http.Request) {
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part id", "BAD_REQUEST")
		return
	}

	err := h.Queries.RemoveBOMSubstitute(r.Context(), dbgen.RemoveBOMSubstituteParams{
		BomLineItemID: itemID,
		PartID:        partID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

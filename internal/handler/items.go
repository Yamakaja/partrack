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
// Body: {resolved_part_id?} — manually resolve or re-resolve a BOM line item.
// Pass null to clear the resolution.
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var resolvedPartID pgtype.UUID
	if body.ResolvedPartID != nil {
		if err := resolvedPartID.Scan(*body.ResolvedPartID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid resolved_part_id", "BAD_REQUEST")
			return
		}
	}

	row, err := h.Queries.UpdateBOMLineItem(r.Context(), dbgen.UpdateBOMLineItemParams{
		ResolvedPartID: resolvedPartID,
		ID:             itemID,
		BomRevisionID:  revID,
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

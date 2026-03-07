package handler

import (
	"encoding/json"
	"net/http"
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
			PrimaryParams: params,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"categories": items})
}

package handler

import (
	"encoding/json"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListLineItemSubstitutes handles GET .../items/{itemId}/substitutes
// Returns substitutes ordered by priority for a single line item.
func (h *Handler) ListLineItemSubstitutes(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	rows, err := h.Queries.ListSubstitutesForLineItem(r.Context(), dbgen.ListSubstitutesForLineItemParams{
		BomLineItemID: itemID,
		TeamID:        teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type subItem struct {
		PartID          string  `json:"part_id"`
		PartMPN         *string `json:"part_mpn"`
		Priority        int32   `json:"priority"`
		Manufacturer    *string `json:"manufacturer"`
		Description     *string `json:"description"`
		LifecycleStatus string  `json:"lifecycle_status"`
		QuantityOnHand  *int32  `json:"quantity_on_hand"`
	}
	items := make([]subItem, len(rows))
	for i, row := range rows {
		items[i] = subItem{
			PartID:          row.PartID.String(),
			PartMPN:         row.PartMpn,
			Priority:        row.Priority,
			Manufacturer:    row.Manufacturer,
			Description:     row.Description,
			LifecycleStatus: string(row.LifecycleStatus),
			QuantityOnHand:  row.QuantityOnHand,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ReorderSubstitutes handles PUT .../items/{itemId}/substitutes/order
// Body: {part_ids: string[]} — sets priorities in the given order (index 0 = highest priority).
func (h *Handler) ReorderSubstitutes(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
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
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	var body struct {
		PartIDs []string `json:"part_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if len(body.PartIDs) == 0 {
		writeError(w, http.StatusBadRequest, "part_ids is required", "BAD_REQUEST")
		return
	}

	partIDs := make([]pgtype.UUID, len(body.PartIDs))
	for i, s := range body.PartIDs {
		if err := partIDs[i].Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid part_id: "+s, "BAD_REQUEST")
			return
		}
	}

	if err := h.Queries.ReorderSubstitutes(r.Context(), dbgen.ReorderSubstitutesParams{
		BomLineItemID: itemID,
		Column2:       partIDs,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ImportSubstitutes handles POST .../items/{itemId}/substitutes/import
// Body: {source_revision_id} — copies substitutes from another revision's equivalent line item.
func (h *Handler) ImportSubstitutes(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
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
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	var body struct {
		SourceRevisionID string `json:"source_revision_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.SourceRevisionID == "" {
		writeError(w, http.StatusBadRequest, "source_revision_id is required", "BAD_REQUEST")
		return
	}

	var sourceRevID pgtype.UUID
	if err := sourceRevID.Scan(body.SourceRevisionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid source_revision_id", "BAD_REQUEST")
		return
	}
	if !h.requireRevisionInTeam(w, r, sourceRevID, teamID) {
		return
	}

	if _, err := h.Queries.ImportSubstitutesFromRevision(r.Context(), dbgen.ImportSubstitutesFromRevisionParams{
		BomLineItemID:  itemID,
		BomRevisionID: sourceRevID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ListPreviousRevisionsWithSubstitutes handles GET .../items/{itemId}/substitutes/previous-revisions
// Returns revisions in the same project that have substitutes for this line item's part.
func (h *Handler) ListPreviousRevisionsWithSubstitutes(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
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
	if !h.requireProjectInTeam(w, r, projectID, teamID) || !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	// Fetch the line item's resolved_part_id.
	lineItems, err := h.Queries.ListBOMLineItems(r.Context(), revID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	var resolvedPartID pgtype.UUID
	for _, li := range lineItems {
		if li.ID == itemID {
			resolvedPartID = li.ResolvedPartID
			break
		}
	}
	if !resolvedPartID.Valid {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}

	rows, err := h.Queries.ListRevisionsWithSubstitutesForPart(r.Context(), dbgen.ListRevisionsWithSubstitutesForPartParams{
		ResolvedPartID: resolvedPartID,
		ProjectID:      projectID,
		ID:             revID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type revItem struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		ImportedAt string `json:"imported_at"`
	}
	items := make([]revItem, len(rows))
	for i, row := range rows {
		items[i] = revItem{
			ID:         row.ID.String(),
			Label:      row.Label,
			ImportedAt: row.ImportedAt.Time.UTC().Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// PatchBuildSubstituteSelections handles PATCH /teams/{teamId}/builds/{buildId}/substitute-selections
// Updates substitute selections for a draft build. Warns if stock is insufficient.
func (h *Handler) PatchBuildSubstituteSelections(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var buildID pgtype.UUID
	if err := buildID.Scan(chi.URLParam(r, "buildId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid build id", "BAD_REQUEST")
		return
	}

	var body struct {
		Selections []struct {
			BomLineItemID string `json:"bom_line_item_id"`
			PartID        string `json:"part_id"`
		} `json:"selections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	// Verify build is draft.
	build, err := h.Queries.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
		ID:     buildID,
		TeamID: teamID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
		return
	}
	if build.Status != "draft" {
		writeError(w, http.StatusConflict, "substitute selections can only be changed for draft builds", "INVALID_STATUS")
		return
	}

	type warning struct {
		BomLineItemID string `json:"bom_line_item_id"`
		PartID        string `json:"part_id"`
		Message       string `json:"message"`
	}
	var warnings []warning

	// Get reserved quantities for all parts in this revision.
	lineItems, err := h.Queries.ListBOMLineItems(r.Context(), build.BomRevisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	requiredByItem := make(map[string]int32)
	for _, li := range lineItems {
		requiredByItem[li.ID.String()] = li.Quantity * build.Quantity
	}

	for _, sel := range body.Selections {
		var liID pgtype.UUID
		if err := liID.Scan(sel.BomLineItemID); err != nil {
			continue
		}
		var partID pgtype.UUID
		if err := partID.Scan(sel.PartID); err != nil {
			continue
		}

		if err := h.Queries.SetBuildSubstituteSelection(r.Context(), dbgen.SetBuildSubstituteSelectionParams{
			BuildID:        buildID,
			BomLineItemID:  liID,
			SelectedPartID: partID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		// Check stock availability; warn but don't block.
		invRows, err := h.Queries.GetInventoryQuantitiesForRevision(r.Context(),
			dbgen.GetInventoryQuantitiesForRevisionParams{
				BomRevisionID: build.BomRevisionID,
				TeamID:        teamID,
			})
		if err == nil {
			for _, inv := range invRows {
				if inv.PartID == partID {
					required := requiredByItem[sel.BomLineItemID]
					free := inv.QuantityOnHand
					if free < required {
						warnings = append(warnings, warning{
							BomLineItemID: sel.BomLineItemID,
							PartID:        sel.PartID,
							Message:       "Insufficient free stock",
						})
					}
					break
				}
			}
		}
	}

	// Reload updated selections.
	updatedSels, err := h.Queries.GetBuildSubstituteSelections(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type selResp struct {
		BomLineItemID   string  `json:"bom_line_item_id"`
		SelectedPartID  string  `json:"selected_part_id"`
		SelectedPartMpn *string `json:"selected_part_mpn"`
	}
	sels := make([]selResp, len(updatedSels))
	for i, s := range updatedSels {
		sels[i] = selResp{
			BomLineItemID:   s.BomLineItemID.String(),
			SelectedPartID:  s.SelectedPartID.String(),
			SelectedPartMpn: s.SelectedPartMpn,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"selections": sels,
		"warnings":   warnings,
	})
}

// RefreshBuildSubstituteSelections handles POST /teams/{teamId}/builds/{buildId}/substitute-selections/refresh
// Re-enqueues the substitute selection job for a draft build.
func (h *Handler) RefreshBuildSubstituteSelections(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var buildID pgtype.UUID
	if err := buildID.Scan(chi.URLParam(r, "buildId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid build id", "BAD_REQUEST")
		return
	}

	// Verify build exists and belongs to this team.
	build, err := h.Queries.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
		ID:     buildID,
		TeamID: teamID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
		return
	}
	if build.Status != "draft" {
		writeError(w, http.StatusConflict, "can only refresh selections for draft builds", "INVALID_STATUS")
		return
	}

	// Clear existing selections and checked_at so the job can repopulate.
	if err := h.Queries.DeleteBuildSubstituteSelections(r.Context(), buildID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Reset selections_checked_at to NULL so submit gate works.
	nilTs := pgtype.Timestamptz{}
	if err := h.Queries.SetBuildSelectionsCheckedAt(r.Context(), dbgen.SetBuildSelectionsCheckedAtParams{
		ID:                  buildID,
		SelectionsCheckedAt: nilTs,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.Worker.Enqueue(worker.JobBuildSubstituteSelections, teamID.String(), map[string]any{
		"build_id": buildID.String(),
		"team_id":  teamID.String(),
	})

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "refreshing"})
}

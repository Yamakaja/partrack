package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type buildDeductionInput struct {
	BomLineItemID string `json:"bom_line_item_id"`
	PartID        string `json:"part_id"`
}

type buildDeduction struct {
	PartID           pgtype.UUID
	InventoryEntryID pgtype.UUID
	Delta            int32
}

// computeDeductions calculates per-part stock deltas for a build.
// substituteSelections maps bom_line_item_id → part_id override.
func (h *Handler) computeDeductions(
	r *http.Request,
	teamID pgtype.UUID,
	revID pgtype.UUID,
	boardsBuilt int32,
	substituteSelections []buildDeductionInput,
) ([]buildDeduction, error) {
	lineItems, err := h.Queries.ListBOMLineItems(r.Context(), revID)
	if err != nil {
		return nil, err
	}

	// Build substitute override map.
	subOverride := make(map[string]pgtype.UUID)
	for _, s := range substituteSelections {
		var partID pgtype.UUID
		if err := partID.Scan(s.PartID); err != nil {
			continue
		}
		subOverride[s.BomLineItemID] = partID
	}

	var result []buildDeduction
	for _, li := range lineItems {
		var partID pgtype.UUID
		if override, ok := subOverride[li.ID.String()]; ok {
			partID = override
		} else if li.ResolvedPartID.Valid {
			partID = li.ResolvedPartID
		} else {
			continue
		}

		entry, err := h.Queries.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
			TeamID: teamID,
			PartID: partID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // no inventory entry, skip
			}
			return nil, err
		}

		result = append(result, buildDeduction{
			PartID:           partID,
			InventoryEntryID: entry.ID,
			Delta:            -(li.Quantity * boardsBuilt),
		})
	}

	return result, nil
}

// ListBuilds handles GET /api/v1/teams/{teamId}/builds
// Query: project_id?, limit, offset
func (h *Handler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var projectID pgtype.UUID
	if s := r.URL.Query().Get("project_id"); s != "" {
		if err := projectID.Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id", "BAD_REQUEST")
			return
		}
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountBuilds(r.Context(), dbgen.CountBuildsParams{
		TeamID:    teamID,
		ProjectID: projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListBuilds(r.Context(), dbgen.ListBuildsParams{
		TeamID:    teamID,
		ProjectID: projectID,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type buildItem struct {
		ID                    string  `json:"id"`
		ProjectID             string  `json:"project_id"`
		ProjectName           string  `json:"project_name"`
		BomRevisionID         string  `json:"bom_revision_id"`
		BomRevisionLabel      string  `json:"bom_revision_label"`
		BoardsBuilt           int32   `json:"boards_built"`
		BuiltAt               string  `json:"built_at"`
		Notes                 *string `json:"notes"`
		RecordedByDisplayName string  `json:"recorded_by_display_name"`
	}

	items := make([]buildItem, len(rows))
	for i, b := range rows {
		items[i] = buildItem{
			ID:                    b.ID.String(),
			ProjectID:             b.ProjectID.String(),
			ProjectName:           b.ProjectName,
			BomRevisionID:         b.BomRevisionID.String(),
			BomRevisionLabel:      b.BomRevisionLabel,
			BoardsBuilt:           b.BoardsBuilt,
			BuiltAt:               b.BuiltAt.Time.UTC().Format(time.RFC3339),
			Notes:                 b.Notes,
			RecordedByDisplayName: b.RecordedByDisplayName,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// PreviewBuild handles POST /api/v1/teams/{teamId}/builds/preview
// Body: {project_id, bom_revision_id, boards_built, substitute_selections?}
// Returns list of {part_id, inventory_entry_id, delta} without committing.
func (h *Handler) PreviewBuild(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var body struct {
		BomRevisionID        string                `json:"bom_revision_id"`
		BoardsBuilt          int32                 `json:"boards_built"`
		SubstituteSelections []buildDeductionInput `json:"substitute_selections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.BomRevisionID == "" || body.BoardsBuilt <= 0 {
		writeError(w, http.StatusBadRequest, "bom_revision_id and boards_built (>0) are required", "BAD_REQUEST")
		return
	}

	var revID pgtype.UUID
	if err := revID.Scan(body.BomRevisionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid bom_revision_id", "BAD_REQUEST")
		return
	}

	deductions, err := h.computeDeductions(r, teamID, revID, body.BoardsBuilt, body.SubstituteSelections)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type deductionResp struct {
		PartID           string `json:"part_id"`
		InventoryEntryID string `json:"inventory_entry_id"`
		Delta            int32  `json:"delta"`
	}

	resp := make([]deductionResp, len(deductions))
	for i, d := range deductions {
		resp[i] = deductionResp{
			PartID:           d.PartID.String(),
			InventoryEntryID: d.InventoryEntryID.String(),
			Delta:            d.Delta,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"deductions": resp})
}

// CreateBuild handles POST /api/v1/teams/{teamId}/builds
// Records the build and commits stock deductions.
func (h *Handler) CreateBuild(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
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

	var body struct {
		ProjectID            string                `json:"project_id"`
		BomRevisionID        string                `json:"bom_revision_id"`
		BoardsBuilt          int32                 `json:"boards_built"`
		Notes                *string               `json:"notes"`
		SubstituteSelections []buildDeductionInput `json:"substitute_selections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.ProjectID == "" || body.BomRevisionID == "" || body.BoardsBuilt <= 0 {
		writeError(w, http.StatusBadRequest, "project_id, bom_revision_id, and boards_built (>0) are required", "BAD_REQUEST")
		return
	}

	var projectID pgtype.UUID
	if err := projectID.Scan(body.ProjectID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project_id", "BAD_REQUEST")
		return
	}
	var revID pgtype.UUID
	if err := revID.Scan(body.BomRevisionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid bom_revision_id", "BAD_REQUEST")
		return
	}

	deductions, err := h.computeDeductions(r, teamID, revID, body.BoardsBuilt, body.SubstituteSelections)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	build, err := qtx.CreateBuild(r.Context(), dbgen.CreateBuildParams{
		TeamID:        teamID,
		ProjectID:     projectID,
		BomRevisionID: revID,
		BoardsBuilt:   body.BoardsBuilt,
		Notes:         body.Notes,
		RecordedBy:    userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	for _, d := range deductions {
		if _, err := qtx.CreateBuildStockAdjustment(r.Context(), dbgen.CreateBuildStockAdjustmentParams{
			InventoryEntryID: d.InventoryEntryID,
			Delta:            d.Delta,
			CreatedBy:        userID,
			BuildID:          build.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if _, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
			ID:             d.InventoryEntryID,
			QuantityOnHand: d.Delta,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":               build.ID.String(),
		"project_id":       build.ProjectID.String(),
		"bom_revision_id":  build.BomRevisionID.String(),
		"boards_built":     build.BoardsBuilt,
		"built_at":         build.BuiltAt.Time.UTC().Format(time.RFC3339),
		"notes":            build.Notes,
		"deduction_count":  len(deductions),
	})
}

// GetBuild handles GET /api/v1/teams/{teamId}/builds/{buildId}
func (h *Handler) GetBuild(w http.ResponseWriter, r *http.Request) {
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

	build, err := h.Queries.GetBuild(r.Context(), dbgen.GetBuildParams{
		ID:     buildID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	deductions, err := h.Queries.ListBuildDeductions(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type deductionResp struct {
		ID               string  `json:"id"`
		InventoryEntryID string  `json:"inventory_entry_id"`
		Delta            int32   `json:"delta"`
		PartID           string  `json:"part_id"`
		PartMpn          *string `json:"part_mpn"`
	}

	deds := make([]deductionResp, len(deductions))
	for i, d := range deductions {
		deds[i] = deductionResp{
			ID:               d.ID.String(),
			InventoryEntryID: d.InventoryEntryID.String(),
			Delta:            d.Delta,
			PartID:           d.PartID.String(),
			PartMpn:          d.PartMpn,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                       build.ID.String(),
		"project_id":               build.ProjectID.String(),
		"project_name":             build.ProjectName,
		"bom_revision_id":          build.BomRevisionID.String(),
		"bom_revision_label":       build.BomRevisionLabel,
		"boards_built":             build.BoardsBuilt,
		"built_at":                 build.BuiltAt.Time.UTC().Format(time.RFC3339),
		"notes":                    build.Notes,
		"recorded_by_display_name": build.RecordedByDisplayName,
		"deductions":               deds,
	})
}

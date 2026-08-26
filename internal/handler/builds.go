package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	"github.com/Yamakaja/partrack/internal/apitypes"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type buildDeductionInput struct {
	BomLineItemID string `json:"bom_line_item_id"`
	PartID        string `json:"part_id"`
}

type buildDeduction struct {
	BomLineItemID        pgtype.UUID
	PartID               pgtype.UUID
	PartMpn              *string
	ReferenceDesignators string
	InventoryEntryID     pgtype.UUID
	Delta                int32 // negative, based on effective (post-exclusion) quantity
	QuantityOnHand       int32
	QuantityPerBoard     int32
	EffectivePerBoard    int32 // after designator exclusions
	AllowShortfall       bool  // true for "use_available" override: cap deduction at on_hand
}

// buildDeductionOpts carries per-build exclusion/override state for computeDeductions.
type buildDeductionOpts struct {
	ExcludedSet map[string]struct{} // individual excluded designators
	Overrides   map[string]string   // bom_line_item_id → "skip" | "use_available"
}

// computeDeductions calculates per-part stock deltas for a build.
// substituteSelections maps bom_line_item_id → part_id override.
// opts is optional; nil means no exclusions/overrides.
func (h *Handler) computeDeductions(
	r *http.Request,
	teamID pgtype.UUID,
	revID pgtype.UUID,
	boardsBuilt int32,
	substituteSelections []buildDeductionInput,
	opts *buildDeductionOpts,
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
		if li.Ignored {
			continue
		}

		// Apply override: "skip" means do not deduct this line item at all.
		allowShortfall := false
		if opts != nil {
			if ov, ok := opts.Overrides[li.ID.String()]; ok {
				switch ov {
				case "skip":
					continue
				case "use_available":
					allowShortfall = true
				}
			}
		}

		// Compute effective quantity per board after designator exclusions.
		effectivePerBoard := li.Quantity
		if opts != nil && len(opts.ExcludedSet) > 0 && li.ReferenceDesignators != "" {
			if total := totalDesignatorCount(li.ReferenceDesignators); total > 0 {
				included := effectiveDesignatorCount(li.ReferenceDesignators, opts.ExcludedSet)
				effectivePerBoard = int32(min(int(li.Quantity), included))
			}
		}
		if effectivePerBoard <= 0 {
			continue // all designators excluded
		}

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
			BomLineItemID:        li.ID,
			PartID:               partID,
			PartMpn:              li.ResolvedPartMpn,
			ReferenceDesignators: li.ReferenceDesignators,
			InventoryEntryID:     entry.ID,
			Delta:                -(effectivePerBoard * boardsBuilt),
			QuantityOnHand:       entry.QuantityOnHand,
			QuantityPerBoard:     li.Quantity,
			EffectivePerBoard:    effectivePerBoard,
			AllowShortfall:       allowShortfall,
		})
	}

	return result, nil
}

// validSortColumns lists the allowed sort_by values for ListBuilds.
var validBuildSortColumns = map[string]bool{
	"project_name": true, "bom_revision_label": true,
	"status": true, "quantity": true, "created_at": true,
}

// ListBuilds handles GET /api/v1/teams/{teamId}/builds
// Query: project_id?, status? (repeatable), created_by?, sort_by?, sort_dir?, limit, offset
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

	statuses := r.URL.Query()["status"]

	var createdByFilter pgtype.UUID
	if s := r.URL.Query().Get("created_by"); s != "" {
		if err := createdByFilter.Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid created_by", "BAD_REQUEST")
			return
		}
	}

	var sortBy, sortDir *string
	if s := r.URL.Query().Get("sort_by"); s != "" && validBuildSortColumns[s] {
		sortBy = &s
		dir := r.URL.Query().Get("sort_dir")
		if dir != "desc" {
			dir = "asc"
		}
		sortDir = &dir
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountBuilds(r.Context(), dbgen.CountBuildsParams{
		TeamID:          teamID,
		ProjectID:       projectID,
		Statuses:        statuses,
		CreatedByFilter: createdByFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListBuilds(r.Context(), dbgen.ListBuildsParams{
		TeamID:          teamID,
		ProjectID:       projectID,
		Statuses:        statuses,
		CreatedByFilter: createdByFilter,
		SortBy:          sortBy,
		SortDir:         sortDir,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type buildItem struct {
		ID                     string  `json:"id"`
		ProjectID              string  `json:"project_id"`
		ProjectName            string  `json:"project_name"`
		BomRevisionID          string  `json:"bom_revision_id"`
		BomRevisionLabel       string  `json:"bom_revision_label"`
		Quantity               int32   `json:"quantity"`
		Status                 string  `json:"status"`
		AssembledAt            *string `json:"assembled_at"`
		CreatedAt              string  `json:"created_at"`
		Notes                  *string `json:"notes"`
		CreatedBy              string  `json:"created_by"`
		CreatedByDisplayName   *string `json:"created_by_display_name"`
		AssembledByDisplayName *string `json:"assembled_by_display_name"`
	}

	items := make([]buildItem, len(rows))
	for i, b := range rows {
		var assembledAt *string
		if b.AssembledAt.Valid {
			t := b.AssembledAt.Time.UTC().Format(time.RFC3339)
			assembledAt = &t
		}
		items[i] = buildItem{
			ID:                     b.ID.String(),
			ProjectID:              b.ProjectID.String(),
			ProjectName:            b.ProjectName,
			BomRevisionID:          b.BomRevisionID.String(),
			BomRevisionLabel:       b.BomRevisionLabel,
			Quantity:               b.Quantity,
			Status:                 b.Status,
			AssembledAt:            assembledAt,
			CreatedAt:              b.CreatedAt.Time.UTC().Format(time.RFC3339),
			Notes:                  b.Notes,
			CreatedBy:              b.CreatedBy.String(),
			CreatedByDisplayName:   b.CreatedByDisplayName,
			AssembledByDisplayName: b.AssembledByDisplayName,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

type pipelineBuild struct {
	ID                   string  `json:"id"`
	ProjectID            string  `json:"project_id"`
	ProjectName          string  `json:"project_name"`
	BomRevisionID        string  `json:"bom_revision_id"`
	BomRevisionLabel     string  `json:"bom_revision_label"`
	Quantity             int32   `json:"quantity"`
	Status               string  `json:"status"`
	CreatedAt            string  `json:"created_at"`
	Notes                *string `json:"notes"`
	CreatedBy            string  `json:"created_by"`
	CreatedByDisplayName *string `json:"created_by_display_name"`
	ExcludedFromCart     bool    `json:"excluded_from_cart"`
}

// GetBuildPipeline handles GET /api/v1/teams/{teamId}/builds/pipeline
// Returns all active (non-terminal) builds grouped by status.
func (h *Handler) GetBuildPipeline(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.GetBuildPipeline(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	result := map[string][]pipelineBuild{
		"draft": {}, "pending": {}, "ordered": {}, "ready": {},
	}

	for _, row := range rows {
		if _, ok := result[row.Status]; !ok {
			continue
		}
		result[row.Status] = append(result[row.Status], pipelineBuild{
			ID:                   row.ID.String(),
			ProjectID:            row.ProjectID.String(),
			ProjectName:          row.ProjectName,
			BomRevisionID:        row.BomRevisionID.String(),
			BomRevisionLabel:     row.BomRevisionLabel,
			Quantity:             row.Quantity,
			Status:               row.Status,
			CreatedAt:            row.CreatedAt.Time.UTC().Format(time.RFC3339),
			Notes:                row.Notes,
			CreatedBy:            row.CreatedBy.String(),
			CreatedByDisplayName: row.CreatedByDisplayName,
			ExcludedFromCart:     row.ExcludedFromCart,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// GetBuildPreview handles GET /api/v1/teams/{teamId}/builds/{buildId}/preview
// Returns per-line-item preview for a saved build, incorporating its excluded_designators
// and line_item_overrides to show effective quantities and stock status.
func (h *Handler) GetBuildPreview(w http.ResponseWriter, r *http.Request) {
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

	build, err := h.Queries.GetBuild(r.Context(), dbgen.GetBuildParams{ID: buildID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	overrideRows, err := h.Queries.GetBuildLineItemOverrides(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	overrideMap := make(map[string]string, len(overrideRows))
	for _, ov := range overrideRows {
		overrideMap[ov.BomLineItemID.String()] = ov.OverrideType
	}

	excludedSet := buildExcludedSet(build.ExcludedDesignators)

	lineItems, err := h.Queries.ListBOMLineItems(r.Context(), build.BomRevisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	storedSels, err := h.Queries.GetBuildSubstituteSelections(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	subSelMap := make(map[string]pgtype.UUID, len(storedSels))
	for _, s := range storedSels {
		subSelMap[s.BomLineItemID.String()] = s.SelectedPartID
	}

	type subItem struct {
		PartID          string  `json:"part_id"`
		PartMpn         *string `json:"part_mpn"`
		PartDescription *string `json:"part_description"`
		QuantityOnHand  int32   `json:"quantity_on_hand"`
	}
	type previewItem struct {
		BomLineItemID               string    `json:"bom_line_item_id"`
		ReferenceDesignators        string    `json:"reference_designators"`
		ResolvedPartID              *string   `json:"resolved_part_id"`
		ResolvedPartMpn             *string   `json:"resolved_part_mpn"`
		ResolvedPartManufacturer    *string   `json:"resolved_part_manufacturer"`
		ResolvedPartLifecycleStatus *string   `json:"resolved_part_lifecycle_status"`
		ResolvedPartImageURL        *string   `json:"resolved_part_image_url"`
		ResolvedPartDescription     *string   `json:"resolved_part_description"`
		QuantityPerBoard            int32     `json:"quantity_per_board"`
		EffectiveQuantityPerBoard   int32     `json:"effective_quantity_per_board"`
		TotalQuantity               int32     `json:"total_quantity"`
		EffectiveTotalQuantity      int32     `json:"effective_total_quantity"`
		QuantityOnHand              int32     `json:"quantity_on_hand"`
		Sufficient                  bool      `json:"sufficient"`
		ExcludedDesignatorCount     int       `json:"excluded_designator_count"`
		Override                    *string   `json:"override"`
		AvailableQuantity           *int32    `json:"available_quantity"`
		Substitutes                 []subItem `json:"substitutes"`
	}

	var items []previewItem
	for _, li := range lineItems {
		if li.Ignored {
			continue
		}

		effectivePerBoard := li.Quantity
		excludedCount := 0
		if len(excludedSet) > 0 && li.ReferenceDesignators != "" {
			if total := totalDesignatorCount(li.ReferenceDesignators); total > 0 {
				included := effectiveDesignatorCount(li.ReferenceDesignators, excludedSet)
				excludedCount = total - included
				effectivePerBoard = int32(min(int(li.Quantity), included))
			}
		}

		override := overrideMap[li.ID.String()]

		// Resolve part: substitute selection overrides primary.
		var resolvedPartID *string
		var resolvedPartMpn *string
		var resolvedPartManufacturer *string
		var resolvedPartLifecycle *string
		var resolvedPartImageURL *string
		var resolvedPartDescription *string
		partID := li.ResolvedPartID
		if sub, ok := subSelMap[li.ID.String()]; ok {
			partID = sub
		}

		var qtyOnHand int32
		if li.ResolvedPartID.Valid {
			s := li.ResolvedPartID.String()
			resolvedPartID = &s
			resolvedPartMpn = li.ResolvedPartMpn
			resolvedPartManufacturer = li.ResolvedPartManufacturer
			if li.ResolvedPartLifecycleStatus.Valid {
				ls := string(li.ResolvedPartLifecycleStatus.PartLifecycleStatus)
				resolvedPartLifecycle = &ls
			}
			resolvedPartImageURL = li.ResolvedPartImageUrl
			resolvedPartDescription = strPtrOrNil(li.ResolvedPartDescription)
		}
		if partID.Valid {
			entry, err := h.Queries.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
				TeamID: teamID,
				PartID: partID,
			})
			if err == nil {
				qtyOnHand = entry.QuantityOnHand
			}
		}

		effectiveTotal := effectivePerBoard * build.Quantity
		sufficient := qtyOnHand >= effectiveTotal || override == "skip" || override == "use_available"

		var overridePtr *string
		var availQty *int32
		if override != "" {
			overridePtr = &override
			if override == "use_available" {
				avail := min(qtyOnHand, effectiveTotal)
				availQty = &avail
			}
		}

		// Fetch substitutes for this line item.
		subs, err := h.Queries.ListSubstitutesForLineItem(r.Context(), dbgen.ListSubstitutesForLineItemParams{
			BomLineItemID: li.ID,
			TeamID:        teamID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		subItems := make([]subItem, len(subs))
		for j, s := range subs {
			var subQty int32
			if s.QuantityOnHand != nil {
				subQty = *s.QuantityOnHand
			}
			subItems[j] = subItem{
				PartID:          s.PartID.String(),
				PartMpn:         s.PartMpn,
				PartDescription: s.Description,
				QuantityOnHand:  subQty,
			}
		}

		items = append(items, previewItem{
			BomLineItemID:               li.ID.String(),
			ReferenceDesignators:        li.ReferenceDesignators,
			ResolvedPartID:              resolvedPartID,
			ResolvedPartMpn:             resolvedPartMpn,
			ResolvedPartManufacturer:    resolvedPartManufacturer,
			ResolvedPartLifecycleStatus: resolvedPartLifecycle,
			ResolvedPartImageURL:        resolvedPartImageURL,
			ResolvedPartDescription:     resolvedPartDescription,
			QuantityPerBoard:            li.Quantity,
			EffectiveQuantityPerBoard:   effectivePerBoard,
			TotalQuantity:               li.Quantity * build.Quantity,
			EffectiveTotalQuantity:      effectiveTotal,
			QuantityOnHand:              qtyOnHand,
			Sufficient:                  sufficient,
			ExcludedDesignatorCount:     excludedCount,
			Override:                    overridePtr,
			AvailableQuantity:           availQty,
			Substitutes:                 subItems,
		})
	}

	if items == nil {
		items = []previewItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// SetBuildExcludedDesignators handles PUT /api/v1/teams/{teamId}/builds/{buildId}/excluded-designators
// Replaces the full set of excluded designators for this build.
// Body: {"designators": ["C1", "R5", "U4"]}
func (h *Handler) SetBuildExcludedDesignators(w http.ResponseWriter, r *http.Request) {
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
		Designators []string `json:"designators"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Designators == nil {
		body.Designators = []string{}
	}

	build, err := h.Queries.GetBuild(r.Context(), dbgen.GetBuildParams{ID: buildID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if build.Status != "draft" && build.Status != "pending" {
		writeError(w, http.StatusConflict, "excluded designators can only be changed on draft or pending builds", "INVALID_STATUS")
		return
	}

	if err := h.Queries.SetBuildExcludedDesignators(r.Context(), dbgen.SetBuildExcludedDesignatorsParams{
		BuildID:     buildID,
		TeamID:      teamID,
		Designators: body.Designators,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if build.Status == "pending" {
		h.CartResolver.Invalidate(teamID.String())
	}
	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// SetBuildLineItemOverride handles PATCH /api/v1/teams/{teamId}/builds/{buildId}/line-item-overrides/{itemId}
// Sets or clears the stock override for one BOM line item on this build.
// Body: {"override_type": "use_available" | "skip" | null}
func (h *Handler) SetBuildLineItemOverride(w http.ResponseWriter, r *http.Request) {
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
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}

	var body struct {
		OverrideType *string `json:"override_type"` // null = remove override
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.OverrideType != nil && *body.OverrideType != "use_available" && *body.OverrideType != "skip" {
		writeError(w, http.StatusBadRequest, "override_type must be 'use_available', 'skip', or null", "BAD_REQUEST")
		return
	}

	build, err := h.Queries.GetBuild(r.Context(), dbgen.GetBuildParams{ID: buildID, TeamID: teamID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "build not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if build.Status != "draft" && build.Status != "pending" {
		writeError(w, http.StatusConflict, "overrides can only be changed on draft or pending builds", "INVALID_STATUS")
		return
	}

	if body.OverrideType == nil {
		if err := h.Queries.DeleteBuildLineItemOverride(r.Context(), dbgen.DeleteBuildLineItemOverrideParams{
			BuildID:       buildID,
			BomLineItemID: itemID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	} else {
		if err := h.Queries.UpsertBuildLineItemOverride(r.Context(), dbgen.UpsertBuildLineItemOverrideParams{
			BuildID:       buildID,
			BomLineItemID: itemID,
			OverrideType:  *body.OverrideType,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if build.Status == "pending" {
		h.CartResolver.Invalidate(teamID.String())
	}
	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// CreateBuild handles POST /api/v1/teams/{teamId}/builds
// Creates a build plan in draft status. No stock deduction.
// Body: {project_id, bom_revision_id, quantity, notes?}
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
		ProjectID     string  `json:"project_id"`
		BomRevisionID string  `json:"bom_revision_id"`
		Quantity      int32   `json:"quantity"`
		Notes         *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.ProjectID == "" || body.BomRevisionID == "" || body.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "project_id, bom_revision_id, and quantity (>0) are required", "BAD_REQUEST")
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
	if !h.requireProjectInTeam(w, r, projectID, teamID) || !h.requireRevisionInTeam(w, r, revID, teamID) {
		return
	}

	build, err := h.Queries.CreateBuildPlan(r.Context(), dbgen.CreateBuildPlanParams{
		TeamID:        teamID,
		ProjectID:     projectID,
		BomRevisionID: revID,
		Quantity:      body.Quantity,
		Notes:         body.Notes,
		CreatedBy:     userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Asynchronously compute substitute selections for this draft build.
	h.Worker.Enqueue(worker.JobBuildSubstituteSelections, teamID.String(), map[string]any{
		"build_id": build.ID.String(),
		"team_id":  teamID.String(),
	})

	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": build.ID.String()})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":              build.ID.String(),
		"project_id":      build.ProjectID.String(),
		"bom_revision_id": build.BomRevisionID.String(),
		"quantity":        build.Quantity,
		"status":          build.Status,
		"notes":           build.Notes,
		"created_at":      build.CreatedAt.Time.UTC().Format(time.RFC3339),
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

	allocations, err := h.Queries.GetBuildPartAllocations(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	orderIDs, err := h.Queries.GetBuildOrderIDs(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	substituteSelections, err := h.Queries.GetBuildSubstituteSelections(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Most recent (non-abandoned) kitting session for the build, if any — lets the
	// desktop restore the kitting panel after reload and hand a completed kitting
	// session off to checkout.
	var kittingSession map[string]any
	if ks, kerr := h.Queries.GetLatestKittingSessionForBuild(r.Context(), buildID); kerr == nil {
		kittingSession = map[string]any{
			"id":     ks.ID.String(),
			"status": ks.Status,
		}
	} else if !errors.Is(kerr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var selectionsStale bool
	if build.SelectionsCheckedAt.Valid {
		stale, err := h.Queries.IsBuildSelectionStale(r.Context(), dbgen.IsBuildSelectionStaleParams{
			BomRevisionID: build.BomRevisionID,
			AdjustedAt:    build.SelectionsCheckedAt,
		})
		if err == nil {
			selectionsStale = stale
		}
	}

	type consumedItemResp struct {
		InventoryEntryID    string  `json:"inventory_entry_id"`
		PartID              string  `json:"part_id"`
		PartMpn             *string `json:"part_mpn"`
		PartManufacturer    *string `json:"part_manufacturer"`
		PartLifecycleStatus string  `json:"part_lifecycle_status"`
		PartImageURL        *string `json:"part_image_url"`
		PartDescription     *string `json:"part_description"`
		QuantityDeducted    int32   `json:"quantity_deducted"`
	}
	var consumedItems []consumedItemResp
	if build.Status == "assembled" {
		deductions, err := h.Queries.ListBuildDeductions(r.Context(), buildID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		consumedItems = make([]consumedItemResp, len(deductions))
		for i, d := range deductions {
			consumedItems[i] = consumedItemResp{
				InventoryEntryID:    d.InventoryEntryID.String(),
				PartID:              d.PartID.String(),
				PartMpn:             d.PartMpn,
				PartManufacturer:    d.PartManufacturer,
				PartLifecycleStatus: string(d.PartLifecycleStatus),
				PartImageURL:        d.PartImageUrl,
				PartDescription:     strPtrOrNil(d.PartDescription),
				QuantityDeducted:    -d.Delta,
			}
		}
	}

	type allocationResp struct {
		ID                  string  `json:"id"`
		PartID              string  `json:"part_id"`
		PartMpn             *string `json:"part_mpn"`
		PartManufacturer    *string `json:"part_manufacturer"`
		PartLifecycleStatus string  `json:"part_lifecycle_status"`
		PartImageURL        *string `json:"part_image_url"`
		PartDescription     *string `json:"part_description"`
		QuantityNeeded      int32   `json:"quantity_needed"`
		QuantityReserved    int32   `json:"quantity_reserved"`
		QuantityReceived    int32   `json:"quantity_received"`
	}
	allocs := make([]allocationResp, len(allocations))
	for i, a := range allocations {
		allocs[i] = allocationResp{
			ID:                  a.ID.String(),
			PartID:              a.PartID.String(),
			PartMpn:             a.PartMpn,
			PartManufacturer:    a.PartManufacturer,
			PartLifecycleStatus: string(a.PartLifecycleStatus),
			PartImageURL:        a.PartImageUrl,
			PartDescription:     strPtrOrNil(a.PartDescription),
			QuantityNeeded:      a.QuantityNeeded,
			QuantityReserved:    a.QuantityReserved,
			QuantityReceived:    a.QuantityReceived,
		}
	}

	orderIDStrs := make([]string, len(orderIDs))
	for i, oid := range orderIDs {
		orderIDStrs[i] = oid.String()
	}

	type subSelectionResp struct {
		BomLineItemID   string  `json:"bom_line_item_id"`
		SelectedPartID  string  `json:"selected_part_id"`
		SelectedPartMpn *string `json:"selected_part_mpn"`
	}
	subSelResp := make([]subSelectionResp, len(substituteSelections))
	for i, s := range substituteSelections {
		subSelResp[i] = subSelectionResp{
			BomLineItemID:   s.BomLineItemID.String(),
			SelectedPartID:  s.SelectedPartID.String(),
			SelectedPartMpn: s.SelectedPartMpn,
		}
	}

	lineItemOverrides, err := h.Queries.GetBuildLineItemOverrides(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	type overrideResp struct {
		BomLineItemID string `json:"bom_line_item_id"`
		OverrideType  string `json:"override_type"`
	}
	overrideResps := make([]overrideResp, len(lineItemOverrides))
	for i, ov := range lineItemOverrides {
		overrideResps[i] = overrideResp{
			BomLineItemID: ov.BomLineItemID.String(),
			OverrideType:  ov.OverrideType,
		}
	}

	resp := map[string]any{
		"id":                        build.ID.String(),
		"project_id":                build.ProjectID.String(),
		"project_name":              build.ProjectName,
		"bom_revision_id":           build.BomRevisionID.String(),
		"bom_revision_label":        build.BomRevisionLabel,
		"quantity":                  build.Quantity,
		"status":                    build.Status,
		"notes":                     build.Notes,
		"excluded_from_cart":        build.ExcludedFromCart,
		"excluded_designators":      build.ExcludedDesignators,
		"created_at":                build.CreatedAt.Time.UTC().Format(time.RFC3339),
		"created_by_display_name":   build.CreatedByDisplayName,
		"assembled_by_display_name": build.AssembledByDisplayName,
		"assembled_quantity":        build.AssembledQuantity,
		"allocations":               allocs,
		"order_ids":                 orderIDStrs,
		"items":                     consumedItems,
		"selections_stale":          selectionsStale,
		"substitute_selections":     subSelResp,
		"line_item_overrides":       overrideResps,
		"kitting_session":           kittingSession,
	}
	if build.AssembledAt.Valid {
		resp["assembled_at"] = build.AssembledAt.Time.UTC().Format(time.RFC3339)
	} else {
		resp["assembled_at"] = nil
	}
	if build.SelectionsCheckedAt.Valid {
		resp["selections_checked_at"] = build.SelectionsCheckedAt.Time.UTC().Format(time.RFC3339)
	} else {
		resp["selections_checked_at"] = nil
	}

	writeJSON(w, http.StatusOK, resp)
}

// GetPartAllocations handles GET /api/v1/teams/{teamId}/parts/{partId}/allocations
// Returns active build allocations (non-assembled, non-cancelled) for a part.
func (h *Handler) GetPartAllocations(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part id", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.GetActiveAllocationsForPart(r.Context(), dbgen.GetActiveAllocationsForPartParams{
		PartID: partID,
		TeamID: teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items := make([]apitypes.PartAllocation, 0, len(rows))
	for _, row := range rows {
		items = append(items, apitypes.PartAllocation{
			BuildID:          row.BuildID.String(),
			ProjectID:        row.ProjectID.String(),
			ProjectName:      row.ProjectName,
			BuildStatus:      row.BuildStatus,
			BuildQuantity:    row.BuildQuantity,
			QuantityNeeded:   row.QuantityNeeded,
			QuantityReserved: row.QuantityReserved,
			QuantityReceived: row.QuantityReceived,
		})
	}

	writeJSON(w, http.StatusOK, apitypes.PartAllocationList{Items: items})
}

// SubmitBuild handles POST /api/v1/teams/{teamId}/builds/{buildId}/submit
// Transitions draft → pending. Triggers recomputeBomShortfalls.
func (h *Handler) SubmitBuild(w http.ResponseWriter, r *http.Request) {
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

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	build, err := qtx.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
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

	if build.Status != "draft" {
		writeError(w, http.StatusConflict, "build must be in draft status to submit", "INVALID_STATUS")
		return
	}

	if !build.SelectionsCheckedAt.Valid {
		writeError(w, http.StatusConflict, "substitute selections are still being computed, please try again shortly", "SUBSTITUTE_SELECTIONS_PENDING")
		return
	}

	// Reject stale selections: if stock has moved since they were computed, the
	// locked choice may reserve against a substitute that no longer has stock.
	// Force a refresh so the reservation math below acts on current inventory.
	stale, err := qtx.IsBuildSelectionStale(r.Context(), dbgen.IsBuildSelectionStaleParams{
		BomRevisionID: build.BomRevisionID,
		AdjustedAt:    build.SelectionsCheckedAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if stale {
		writeError(w, http.StatusConflict, "stock has changed since substitute selections were computed; refresh selections before submitting", "SUBSTITUTE_SELECTIONS_STALE")
		return
	}

	// Reserve free (non-reserved) stock for this build's demand. A build whose
	// every part is fully covered by available stock needs no purchasing and is
	// promoted straight to 'ready'; otherwise it stays 'pending' and its
	// shortfall feeds the cart. Allocations are only persisted for fully-covered
	// builds — pending builds carry none (ConvertCart creates them later), so
	// ConvertCart's reservation math sees no pre-existing reservation for them.
	newStatus, err := h.reserveStockForSubmit(r.Context(), qtx, build, teamID, buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := qtx.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
		ID:     buildID,
		Status: newStatus,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	cart, err := qtx.GetOrCreateCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if _, err := h.recomputeBomShortfalls(r.Context(), qtx, cart.ID, teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "build.submitted",
		Feed:    activity.FeedTeam,
		Subject: &activity.Ref{Type: "build", ID: buildID.String(), Label: h.buildLabel(r.Context(), buildID, teamID)},
		Values:  map[string]any{"quantity": build.Quantity},
		Summary: fmt.Sprintf("Submitted a build of %d board(s)", build.Quantity),
	})
	h.publishCartChanged(r, teamID)
	writeJSON(w, http.StatusOK, map[string]any{"id": buildID.String(), "status": newStatus})
}

// computeBuildDemand aggregates a build's per-part demand from its BOM
// revision: resolved, non-ignored line items honouring designator exclusions,
// skip/use_available overrides, and locked substitute selections (a selection
// wins over the primary part), multiplied by the build quantity. Keys are
// part-ID strings. Shared by submit reservation and kitting seeding — kitting
// must not depend on build_part_allocations, which only exist once a build
// reaches ready/ordered.
func computeBuildDemand(
	ctx context.Context,
	q *dbgen.Queries,
	build dbgen.GetBuildForUpdateRow,
	buildID pgtype.UUID,
) (map[string]int32, error) {
	lineItems, err := q.ListBOMLineItems(ctx, build.BomRevisionID)
	if err != nil {
		return nil, err
	}

	overrideRows, err := q.GetBuildLineItemOverridesForBuilds(ctx, []pgtype.UUID{buildID})
	if err != nil {
		return nil, err
	}
	overrideMap := make(map[string]string, len(overrideRows))
	for _, ov := range overrideRows {
		overrideMap[ov.BomLineItemID.String()] = ov.OverrideType
	}

	selRows, err := q.GetBuildSubstituteSelections(ctx, buildID)
	if err != nil {
		return nil, err
	}
	selectionMap := make(map[string]string, len(selRows))
	for _, s := range selRows {
		selectionMap[s.BomLineItemID.String()] = s.SelectedPartID.String()
	}

	excludedSet := buildExcludedSet(build.ExcludedDesignators)

	// Aggregate demand per part (substitute selection wins over the primary part).
	neededByPart := make(map[string]int32)
	for _, li := range lineItems {
		if !li.ResolvedPartID.Valid || li.Ignored {
			continue
		}
		if ov, ok := overrideMap[li.ID.String()]; ok && (ov == "skip" || ov == "use_available") {
			continue
		}
		effectivePerBoard := li.Quantity
		if len(excludedSet) > 0 && li.ReferenceDesignators != "" {
			if total := totalDesignatorCount(li.ReferenceDesignators); total > 0 {
				included := effectiveDesignatorCount(li.ReferenceDesignators, excludedSet)
				effectivePerBoard = int32(min(int(li.Quantity), included))
			}
		}
		if effectivePerBoard <= 0 {
			continue
		}
		partIDStr := li.ResolvedPartID.String()
		if sel, ok := selectionMap[li.ID.String()]; ok {
			partIDStr = sel
		}
		neededByPart[partIDStr] += effectivePerBoard * build.Quantity
	}
	return neededByPart, nil
}

// reserveStockForSubmit computes a build's per-part demand (honouring designator
// exclusions, skip/use_available overrides, and locked substitute selections),
// then checks whether free (on-hand minus stock already reserved by other
// builds) covers it in full. If so it persists soft reservations on
// build_part_allocations and returns "ready"; otherwise it persists nothing and
// returns "pending". Must be called within the submit transaction.
func (h *Handler) reserveStockForSubmit(
	ctx context.Context,
	qtx *dbgen.Queries,
	build dbgen.GetBuildForUpdateRow,
	teamID, buildID pgtype.UUID,
) (string, error) {
	neededByPart, err := computeBuildDemand(ctx, qtx, build, buildID)
	if err != nil {
		return "", err
	}

	// Decide reservation per part against free (non-reserved) stock.
	type reservation struct {
		partID   pgtype.UUID
		needed   int32
		reserved int32
	}
	reservations := make([]reservation, 0, len(neededByPart))
	fullyCovered := true
	for partStr, needed := range neededByPart {
		var partID pgtype.UUID
		if err := partID.Scan(partStr); err != nil {
			return "", err
		}
		alreadyReserved, err := qtx.SumReservedForPart(ctx, dbgen.SumReservedForPartParams{
			PartID: partID,
			TeamID: teamID,
		})
		if err != nil {
			return "", err
		}
		var onHand int32
		entry, err := qtx.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
			TeamID: teamID,
			PartID: partID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if err == nil {
			onHand = entry.QuantityOnHand
		}
		free := onHand - alreadyReserved
		if free < 0 {
			free = 0
		}
		reserve := needed
		if reserve > free {
			reserve = free
			fullyCovered = false
		}
		reservations = append(reservations, reservation{partID: partID, needed: needed, reserved: reserve})
	}

	if !fullyCovered {
		return "pending", nil
	}

	// Fully covered: persist soft reservations and promote to ready.
	for _, rsv := range reservations {
		if _, err := qtx.UpsertBuildPartAllocation(ctx, dbgen.UpsertBuildPartAllocationParams{
			BuildID:          buildID,
			PartID:           rsv.partID,
			QuantityNeeded:   rsv.needed,
			QuantityReserved: rsv.reserved,
			QuantityReceived: 0,
		}); err != nil {
			return "", err
		}
	}
	return "ready", nil
}

// CheckoutBuild handles POST /api/v1/teams/{teamId}/builds/{buildId}/checkout
// Executes a build in draft or ready status: deducts stock, records assembly.
// Body: {assembled_quantity?, substitute_selections?, allow_stock_reconciliation?}
func (h *Handler) CheckoutBuild(w http.ResponseWriter, r *http.Request) {
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
		AssembledQuantity    *int32                `json:"assembled_quantity"`
		SubstituteSelections []buildDeductionInput `json:"substitute_selections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	build, err := qtx.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
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

	if build.Status != "draft" && build.Status != "ready" {
		writeError(w, http.StatusConflict, "build must be in draft or ready status to checkout", "INVALID_STATUS")
		return
	}

	assembledQty := build.Quantity
	if body.AssembledQuantity != nil {
		assembledQty = *body.AssembledQuantity
	}
	if assembledQty <= 0 || assembledQty > build.Quantity {
		writeError(w, http.StatusBadRequest, "assembled_quantity must be > 0 and <= build quantity", "BAD_REQUEST")
		return
	}

	// If no substitute selections provided in the request body, load stored selections.
	// Orphaned selections (no longer approved substitutes) silently fall back to primary.
	effectiveSelections := body.SubstituteSelections
	if len(effectiveSelections) == 0 {
		storedSels, err := qtx.GetBuildSubstituteSelections(r.Context(), buildID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		for _, sel := range storedSels {
			subs, err := qtx.ListSubstitutesForLineItem(r.Context(), dbgen.ListSubstitutesForLineItemParams{
				BomLineItemID: sel.BomLineItemID,
				TeamID:        teamID,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			for _, s := range subs {
				if s.PartID == sel.SelectedPartID {
					effectiveSelections = append(effectiveSelections, buildDeductionInput{
						BomLineItemID: sel.BomLineItemID.String(),
						PartID:        sel.SelectedPartID.String(),
					})
					break
				}
			}
			// Orphaned selections (part no longer approved) fall back to primary — not added.
		}
	}

	// Load per-build exclusions and overrides.
	lineItemOverrides, err := qtx.GetBuildLineItemOverrides(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	overrideMap := make(map[string]string, len(lineItemOverrides))
	for _, ov := range lineItemOverrides {
		overrideMap[ov.BomLineItemID.String()] = ov.OverrideType
	}
	deductOpts := &buildDeductionOpts{
		ExcludedSet: buildExcludedSet(build.ExcludedDesignators),
		Overrides:   overrideMap,
	}

	deductions, err := h.computeDeductions(r, teamID, build.BomRevisionID, assembledQty, effectiveSelections, deductOpts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type insufficientItem struct {
		BomLineItemID    string  `json:"bom_line_item_id"`
		PartID           string  `json:"part_id"`
		PartMpn          *string `json:"part_mpn"`
		QuantityOnHand   int32   `json:"quantity_on_hand"`
		QuantityRequired int32   `json:"quantity_required"`
	}
	var insufficientItems []insufficientItem

	for i := range deductions {
		d := &deductions[i]
		locked, err := qtx.GetInventoryEntryForUpdate(r.Context(), d.InventoryEntryID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		totalQty := -d.Delta
		if locked.QuantityOnHand < totalQty {
			if d.AllowShortfall {
				// Cap deduction at what is actually on hand (re-validated under lock).
				d.Delta = -locked.QuantityOnHand
			} else {
				insufficientItems = append(insufficientItems, insufficientItem{
					BomLineItemID:    d.BomLineItemID.String(),
					PartID:           d.PartID.String(),
					PartMpn:          d.PartMpn,
					QuantityOnHand:   locked.QuantityOnHand,
					QuantityRequired: totalQty,
				})
				continue
			}
		}

		if d.Delta < 0 {
			if _, err := qtx.CreateBuildStockAdjustment(r.Context(), dbgen.CreateBuildStockAdjustmentParams{
				InventoryEntryID: d.InventoryEntryID,
				Delta:            d.Delta,
				CreatedBy:        userID,
				BuildID:          buildID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}
	}

	if len(insufficientItems) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":              "insufficient stock for some items; use per-item overrides before checking out",
			"code":               "INSUFFICIENT_STOCK",
			"insufficient_items": insufficientItems,
		})
		return
	}

	if assembledQty == build.Quantity {
		// Full checkout: clear allocations and record assembly.
		if err := qtx.DeleteBuildPartAllocations(r.Context(), buildID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		now := pgtype.Timestamptz{}
		_ = now.Scan(time.Now())
		updated, err := qtx.UpdateBuildAssembled(r.Context(), dbgen.UpdateBuildAssembledParams{
			ID:                buildID,
			Status:            "assembled",
			AssembledQuantity: &assembledQty,
			AssembledAt:       now,
			AssembledBy:       userID,
			TeamID:            teamID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
		h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{})
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                 updated.ID.String(),
			"status":             updated.Status,
			"assembled_quantity": updated.AssembledQuantity,
			"assembled_at":       updated.AssembledAt.Time.UTC().Format(time.RFC3339),
			"deduction_count":    len(deductions),
		})
		return
	}

	// Partial checkout: adjust allocations, decrement quantity, re-evaluate readiness.
	for _, d := range deductions {
		consumed := -d.Delta // positive
		if err := qtx.AdjustBuildPartAllocationAfterPartialCheckout(r.Context(), dbgen.AdjustBuildPartAllocationAfterPartialCheckoutParams{
			Consumed: consumed,
			BuildID:  buildID,
			PartID:   d.PartID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	remaining, err := qtx.DecrementBuildQuantity(r.Context(), dbgen.DecrementBuildQuantityParams{
		AssembledQuantity: assembledQty,
		ID:                buildID,
		TeamID:            teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	isReady, err := qtx.CheckBuildReadiness(r.Context(), buildID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	newStatus := "ordered"
	if isReady {
		newStatus = "ready"
	}
	if err := qtx.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
		ID:     buildID,
		Status: newStatus,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{})
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "build.assembled",
		Feed:    activity.FeedTeam,
		Subject: &activity.Ref{Type: "build", ID: buildID.String(), Label: h.buildLabel(r.Context(), buildID, teamID)},
		Values:  map[string]any{"parts_deducted": len(deductions)},
		Summary: fmt.Sprintf("Assembled a build (%d part(s) deducted)", len(deductions)),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                 remaining.ID.String(),
		"status":             newStatus,
		"remaining_quantity": remaining.Quantity,
		"deduction_count":    len(deductions),
	})
}

// UpdateBuild handles PATCH /api/v1/teams/{teamId}/builds/{buildId}
// Body: {quantity?, notes?}. Only allowed for draft or pending builds.
func (h *Handler) UpdateBuild(w http.ResponseWriter, r *http.Request) {
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
		Quantity *int32  `json:"quantity"`
		Notes    *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Quantity == nil && body.Notes == nil {
		writeError(w, http.StatusBadRequest, "at least one of quantity or notes is required", "BAD_REQUEST")
		return
	}
	if body.Quantity != nil && *body.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "quantity must be positive", "BAD_REQUEST")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpdateBuildQuantityAndNotes(r.Context(), dbgen.UpdateBuildQuantityAndNotesParams{
		ID:       buildID,
		TeamID:   teamID,
		Quantity: body.Quantity,
		Notes:    body.Notes,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "build not found or not in draft/pending status", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Recompute shortfalls if the build is pending (quantity change may affect cart demand).
	if updated.Status == "pending" && body.Quantity != nil {
		cart, err := qtx.GetOrCreateCart(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if _, err := h.recomputeBomShortfalls(r.Context(), qtx, cart.ID, teamID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	if updated.Status == "pending" && body.Quantity != nil {
		h.publishCartChanged(r, teamID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       updated.ID.String(),
		"quantity": updated.Quantity,
		"notes":    updated.Notes,
		"status":   updated.Status,
	})
}

// CancelBuild handles DELETE /api/v1/teams/{teamId}/builds/{buildId}
// Cancels any non-terminal build, releases reservations.
func (h *Handler) CancelBuild(w http.ResponseWriter, r *http.Request) {
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

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	build, err := qtx.GetBuildForUpdate(r.Context(), dbgen.GetBuildForUpdateParams{
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

	if build.Status == "assembled" || build.Status == "cancelled" {
		writeError(w, http.StatusConflict, "build is already in a terminal state", "INVALID_STATUS")
		return
	}

	wasPending := build.Status == "pending"

	if err := qtx.DeleteBuildPartAllocations(r.Context(), buildID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := qtx.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
		ID:     buildID,
		Status: "cancelled",
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if wasPending {
		cart, err := qtx.GetOrCreateCart(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if _, err := h.recomputeBomShortfalls(r.Context(), qtx, cart.ID, teamID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "build.cancelled",
		Feed:    activity.FeedTeam,
		Subject: &activity.Ref{Type: "build", ID: buildID.String(), Label: h.buildLabel(r.Context(), buildID, teamID)},
		Summary: "Cancelled a build",
	})
	if wasPending {
		h.publishCartChanged(r, teamID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ToggleBuildCartInclusion handles PATCH /api/v1/teams/{teamId}/builds/{buildId}/cart-inclusion
// Body: {excluded: bool}. Controls whether a pending build contributes to cart demand.
func (h *Handler) ToggleBuildCartInclusion(w http.ResponseWriter, r *http.Request) {
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
		Excluded bool `json:"excluded"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	if err := qtx.SetBuildExcludedFromCart(r.Context(), dbgen.SetBuildExcludedFromCartParams{
		ID:               buildID,
		ExcludedFromCart: body.Excluded,
		TeamID:           teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	cart, err := qtx.GetOrCreateCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if _, err := h.recomputeBomShortfalls(r.Context(), qtx, cart.ID, teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	h.publishCartChanged(r, teamID)
	w.WriteHeader(http.StatusNoContent)
}

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
	BomLineItemID        pgtype.UUID
	PartID               pgtype.UUID
	PartMpn              *string
	ReferenceDesignators string
	InventoryEntryID     pgtype.UUID
	Delta                int32 // negative
	QuantityOnHand       int32
	QuantityPerBoard     int32
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
			BomLineItemID:        li.ID,
			PartID:               partID,
			PartMpn:              li.ResolvedPartMpn,
			ReferenceDesignators: li.ReferenceDesignators,
			InventoryEntryID:     entry.ID,
			Delta:                -(li.Quantity * boardsBuilt),
			QuantityOnHand:       entry.QuantityOnHand,
			QuantityPerBoard:     li.Quantity,
		})
	}

	return result, nil
}

// ListBuilds handles GET /api/v1/teams/{teamId}/builds
// Query: project_id?, status?, limit, offset
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

	var status *string
	if s := r.URL.Query().Get("status"); s != "" {
		status = &s
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountBuilds(r.Context(), dbgen.CountBuildsParams{
		TeamID:    teamID,
		ProjectID: projectID,
		Status:    status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListBuilds(r.Context(), dbgen.ListBuildsParams{
		TeamID:    teamID,
		ProjectID: projectID,
		Status:    status,
		Limit:     limit,
		Offset:    offset,
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

// PreviewBuild handles POST /api/v1/teams/{teamId}/builds/preview
// Body: {bom_revision_id, quantity, substitute_selections?}
// Returns list of deductions without committing.
func (h *Handler) PreviewBuild(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var body struct {
		BomRevisionID        string                `json:"bom_revision_id"`
		Quantity             int32                 `json:"quantity"`
		SubstituteSelections []buildDeductionInput `json:"substitute_selections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.BomRevisionID == "" || body.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "bom_revision_id and quantity (>0) are required", "BAD_REQUEST")
		return
	}

	var revID pgtype.UUID
	if err := revID.Scan(body.BomRevisionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid bom_revision_id", "BAD_REQUEST")
		return
	}

	deductions, err := h.computeDeductions(r, teamID, revID, body.Quantity, body.SubstituteSelections)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type previewItem struct {
		BomLineItemID        string  `json:"bom_line_item_id"`
		ReferenceDesignators string  `json:"reference_designators"`
		ResolvedPartID       string  `json:"resolved_part_id"`
		ResolvedPartMpn      *string `json:"resolved_part_mpn"`
		QuantityPerBoard     int32   `json:"quantity_per_board"`
		TotalQuantity        int32   `json:"total_quantity"`
		QuantityOnHand       int32   `json:"quantity_on_hand"`
		Sufficient           bool    `json:"sufficient"`
	}

	items := make([]previewItem, len(deductions))
	for i, d := range deductions {
		totalQty := -d.Delta
		items[i] = previewItem{
			BomLineItemID:        d.BomLineItemID.String(),
			ReferenceDesignators: d.ReferenceDesignators,
			ResolvedPartID:       d.PartID.String(),
			ResolvedPartMpn:      d.PartMpn,
			QuantityPerBoard:     d.QuantityPerBoard,
			TotalQuantity:        totalQty,
			QuantityOnHand:       d.QuantityOnHand,
			Sufficient:           d.QuantityOnHand >= totalQty,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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

	type allocationResp struct {
		ID               string  `json:"id"`
		PartID           string  `json:"part_id"`
		PartMpn          *string `json:"part_mpn"`
		QuantityNeeded   int32   `json:"quantity_needed"`
		QuantityReserved int32   `json:"quantity_reserved"`
		QuantityReceived int32   `json:"quantity_received"`
	}
	allocs := make([]allocationResp, len(allocations))
	for i, a := range allocations {
		allocs[i] = allocationResp{
			ID:               a.ID.String(),
			PartID:           a.PartID.String(),
			PartMpn:          a.PartMpn,
			QuantityNeeded:   a.QuantityNeeded,
			QuantityReserved: a.QuantityReserved,
			QuantityReceived: a.QuantityReceived,
		}
	}

	orderIDStrs := make([]string, len(orderIDs))
	for i, oid := range orderIDs {
		orderIDStrs[i] = oid.String()
	}

	resp := map[string]any{
		"id":                       build.ID.String(),
		"project_id":               build.ProjectID.String(),
		"project_name":             build.ProjectName,
		"bom_revision_id":          build.BomRevisionID.String(),
		"bom_revision_label":       build.BomRevisionLabel,
		"quantity":                 build.Quantity,
		"status":                   build.Status,
		"notes":                    build.Notes,
		"excluded_from_cart":       build.ExcludedFromCart,
		"created_at":               build.CreatedAt.Time.UTC().Format(time.RFC3339),
		"created_by_display_name":  build.CreatedByDisplayName,
		"assembled_by_display_name": build.AssembledByDisplayName,
		"assembled_quantity":        build.AssembledQuantity,
		"allocations":               allocs,
		"order_ids":                 orderIDStrs,
	}
	if build.AssembledAt.Valid {
		resp["assembled_at"] = build.AssembledAt.Time.UTC().Format(time.RFC3339)
	} else {
		resp["assembled_at"] = nil
	}

	writeJSON(w, http.StatusOK, resp)
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

	if err := qtx.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
		ID:     buildID,
		Status: "pending",
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
	h.publishCartChanged(r, teamID)
	writeJSON(w, http.StatusOK, map[string]any{"id": buildID.String(), "status": "pending"})
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
		AssembledQuantity        *int32                `json:"assembled_quantity"`
		SubstituteSelections     []buildDeductionInput `json:"substitute_selections"`
		AllowStockReconciliation bool                  `json:"allow_stock_reconciliation"`
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

	deductions, err := h.computeDeductions(r, teamID, build.BomRevisionID, assembledQty, body.SubstituteSelections)
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

	for _, d := range deductions {
		locked, err := qtx.GetInventoryEntryForUpdate(r.Context(), d.InventoryEntryID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		totalQty := -d.Delta
		if locked.QuantityOnHand < totalQty {
			if !body.AllowStockReconciliation {
				insufficientItems = append(insufficientItems, insufficientItem{
					BomLineItemID:    d.BomLineItemID.String(),
					PartID:           d.PartID.String(),
					PartMpn:          d.PartMpn,
					QuantityOnHand:   locked.QuantityOnHand,
					QuantityRequired: totalQty,
				})
				continue
			}
			reconcileDelta := totalQty - locked.QuantityOnHand
			reconcileReason := "Stock reconciliation: corrected before build"
			if _, err := qtx.CreateStockAdjustment(r.Context(), dbgen.CreateStockAdjustmentParams{
				InventoryEntryID: d.InventoryEntryID,
				Delta:            reconcileDelta,
				Reason:           &reconcileReason,
				CreatedBy:        userID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if _, err := qtx.ApplyStockDelta(r.Context(), dbgen.ApplyStockDeltaParams{
				ID:             d.InventoryEntryID,
				QuantityOnHand: reconcileDelta,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}

		if _, err := qtx.CreateBuildStockAdjustment(r.Context(), dbgen.CreateBuildStockAdjustmentParams{
			InventoryEntryID: d.InventoryEntryID,
			Delta:            d.Delta,
			CreatedBy:        userID,
			BuildID:          buildID,
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

	if len(insufficientItems) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":              "some parts have insufficient stock; set allow_stock_reconciliation to auto-correct",
			"code":               "INSUFFICIENT_STOCK",
			"insufficient_items": insufficientItems,
		})
		return
	}

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

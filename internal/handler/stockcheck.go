package handler

import (
	"errors"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// StockCheck handles GET /api/v1/teams/{teamId}/projects/{projectId}/stock-check
// Query: build_qty (required). The build_qty is ephemeral — not stored.
// Returns per-line-item status against the project's active BOM revision.
func (h *Handler) StockCheck(w http.ResponseWriter, r *http.Request) {
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

	buildQtyStr := r.URL.Query().Get("build_qty")
	if buildQtyStr == "" {
		writeError(w, http.StatusBadRequest, "build_qty is required", "BAD_REQUEST")
		return
	}
	buildQty64, err := strconv.ParseInt(buildQtyStr, 10, 32)
	if err != nil || buildQty64 <= 0 {
		writeError(w, http.StatusBadRequest, "build_qty must be a positive integer", "BAD_REQUEST")
		return
	}
	buildQty := int32(buildQty64)

	// Fetch the project to get active_bom_revision_id.
	project, err := h.Queries.GetProject(r.Context(), dbgen.GetProjectParams{
		ID:     projectID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if !project.ActiveBomRevisionID.Valid {
		writeError(w, http.StatusUnprocessableEntity, "project has no active BOM revision", "NO_ACTIVE_BOM")
		return
	}

	revID := project.ActiveBomRevisionID

	// Fetch line items, inventory quantities, and substitutes in parallel.
	type lineItemsResult struct {
		items []dbgen.ListBOMLineItemsRow
		err   error
	}
	type inventoryResult struct {
		rows []dbgen.GetInventoryQuantitiesForRevisionRow
		err  error
	}
	type subsResult struct {
		rows []dbgen.ListAllSubstitutesForRevisionRow
		err  error
	}

	liCh := make(chan lineItemsResult, 1)
	invCh := make(chan inventoryResult, 1)
	subsCh := make(chan subsResult, 1)

	go func() {
		items, err := h.Queries.ListBOMLineItems(r.Context(), revID)
		liCh <- lineItemsResult{items, err}
	}()
	go func() {
		rows, err := h.Queries.GetInventoryQuantitiesForRevision(r.Context(),
			dbgen.GetInventoryQuantitiesForRevisionParams{
				BomRevisionID: revID,
				TeamID:        teamID,
			})
		invCh <- inventoryResult{rows, err}
	}()
	go func() {
		rows, err := h.Queries.ListAllSubstitutesForRevision(r.Context(), revID)
		subsCh <- subsResult{rows, err}
	}()

	liRes := <-liCh
	invRes := <-invCh
	subsRes := <-subsCh

	if liRes.err != nil || invRes.err != nil || subsRes.err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Build lookup maps.
	stockByPart := make(map[string]int32)
	for _, row := range invRes.rows {
		stockByPart[row.PartID.String()] = row.QuantityOnHand
	}

	type subInfo struct {
		PartID  string  `json:"part_id"`
		PartMpn *string `json:"part_mpn"`
		Stock   *int32  `json:"stock_on_hand"`
	}
	subsByItem := make(map[string][]subInfo)
	for _, s := range subsRes.rows {
		key := s.BomLineItemID.String()
		pid := s.PartID.String()
		var stockPtr *int32
		if stock, ok := stockByPart[pid]; ok {
			v := stock
			stockPtr = &v
		}
		subsByItem[key] = append(subsByItem[key], subInfo{
			PartID:  pid,
			PartMpn: s.PartMpn,
			Stock:   stockPtr,
		})
	}

	type itemResult struct {
		ID                   string    `json:"id"`
		ReferenceDesignators string    `json:"reference_designators"`
		Quantity             int32     `json:"quantity"`
		RawDescription       *string   `json:"raw_description"`
		RequiredQuantity     int32     `json:"required_quantity"`
		Status               string    `json:"status"`
		ResolvedPartID       *string   `json:"resolved_part_id"`
		ResolvedPartMpn      *string   `json:"resolved_part_mpn"`
		StockOnHand          *int32    `json:"stock_on_hand"`
		Substitutes          []subInfo `json:"substitutes"`
	}

	results := make([]itemResult, len(liRes.items))
	for i, li := range liRes.items {
		id := li.ID.String()
		required := li.Quantity * buildQty
		subs := subsByItem[id]
		if subs == nil {
			subs = []subInfo{}
		}

		var status string
		var stockPtr *int32

		if !li.ResolvedPartID.Valid {
			status = "unresolved"
		} else {
			pid := li.ResolvedPartID.String()
			if stock, ok := stockByPart[pid]; ok {
				v := stock
				stockPtr = &v
				if stock >= required {
					status = "ok"
				} else {
					// Check substitutes to see if combined or any single covers the need.
					covered := false
					for _, s := range subs {
						if s.Stock != nil && *s.Stock >= required {
							covered = true
							break
						}
					}
					if covered {
						status = "ok"
					} else {
						status = "shortfall"
					}
				}
			} else {
				// No inventory entry at all — treat as zero stock.
				status = "shortfall"
			}
		}

		results[i] = itemResult{
			ID:                   id,
			ReferenceDesignators: li.ReferenceDesignators,
			Quantity:             li.Quantity,
			RawDescription:       li.RawDescription,
			RequiredQuantity:     required,
			Status:               status,
			ResolvedPartID:       uuidPtrString(li.ResolvedPartID),
			ResolvedPartMpn:      li.ResolvedPartMpn,
			StockOnHand:          stockPtr,
			Substitutes:          subs,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bom_revision_id": revID.String(),
		"build_qty":       buildQty,
		"items":           results,
	})
}

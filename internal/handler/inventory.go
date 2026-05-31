package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListInventory handles GET /api/v1/teams/{teamId}/inventory
func (h *Handler) ListInventory(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	q := r.URL.Query()
	var search *string
	if v := q.Get("q"); v != "" {
		search = &v
	}

	var storageLocID pgtype.UUID
	if v := q.Get("storage_location_id"); v != "" {
		if err := storageLocID.Scan(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid storage_location_id", "BAD_REQUEST")
			return
		}
	}

	belowMinStock := q.Get("below_min_stock") == "true"
	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountInventory(r.Context(), dbgen.CountInventoryParams{
		TeamID:            teamID,
		Q:                 search,
		StorageLocationID: storageLocID,
		BelowMinStock:     belowMinStock,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListInventory(r.Context(), dbgen.ListInventoryParams{
		TeamID:            teamID,
		Q:                 search,
		StorageLocationID: storageLocID,
		BelowMinStock:     belowMinStock,
		Offset:            offset,
		Limit:             limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID                   string  `json:"id"`
		TeamID               string  `json:"team_id"`
		PartID               string  `json:"part_id"`
		PartMpn              *string `json:"part_mpn"`
		PartDescription      *string `json:"part_description"`
		QuantityOnHand       int32   `json:"quantity_on_hand"`
		StorageLocationID    *string `json:"storage_location_id"`
		StorageLocationLabel *string `json:"storage_location_label"`
		PreferredListingID   *string `json:"preferred_listing_id"`
		MinimumStockQuantity *int32  `json:"minimum_stock_quantity"`
		ReorderQuantity      *int32  `json:"reorder_quantity"`
	}
	items := make([]item, len(rows))
	for i, row := range rows {
		var storLocID *string
		if row.StorageLocationID.Valid {
			s := row.StorageLocationID.String()
			storLocID = &s
		}
		var prefListID *string
		if row.PreferredListingID.Valid {
			s := row.PreferredListingID.String()
			prefListID = &s
		}
		items[i] = item{
			ID:                   row.ID.String(),
			TeamID:               row.TeamID.String(),
			PartID:               row.PartID.String(),
			PartMpn:              row.PartMpn,
			PartDescription:      row.PartDescription,
			QuantityOnHand:       row.QuantityOnHand,
			StorageLocationID:    storLocID,
			StorageLocationLabel: row.StorageLocationLabel,
			PreferredListingID:   prefListID,
			MinimumStockQuantity: row.MinimumStockQuantity,
			ReorderQuantity:      row.ReorderQuantity,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "limit": limit, "offset": offset, "items": items,
	})
}

// CreateInventoryEntry handles POST /api/v1/teams/{teamId}/inventory
func (h *Handler) CreateInventoryEntry(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		PartID               string  `json:"part_id"`
		StorageLocationID    *string `json:"storage_location_id"`
		PreferredListingID   *string `json:"preferred_listing_id"`
		MinimumStockQuantity *int32  `json:"minimum_stock_quantity"`
		ReorderQuantity      *int32  `json:"reorder_quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var partID pgtype.UUID
	if err := partID.Scan(req.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}

	var storLocID pgtype.UUID
	if req.StorageLocationID != nil {
		if err := storLocID.Scan(*req.StorageLocationID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid storage_location_id", "BAD_REQUEST")
			return
		}
	}

	var prefListID pgtype.UUID
	if req.PreferredListingID != nil {
		if err := prefListID.Scan(*req.PreferredListingID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid preferred_listing_id", "BAD_REQUEST")
			return
		}
	}

	entry, err := h.Queries.CreateInventoryEntry(r.Context(), dbgen.CreateInventoryEntryParams{
		TeamID:               teamID,
		PartID:               partID,
		StorageLocationID:    storLocID,
		PreferredListingID:   prefListID,
		MinimumStockQuantity: req.MinimumStockQuantity,
		ReorderQuantity:      req.ReorderQuantity,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "inventory entry already exists for this part", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{"part_id": entry.PartID.String(), "entry_id": entry.ID.String()})
	writeJSON(w, http.StatusCreated, inventoryEntryResponse(entry))
}

// GetInventoryEntry handles GET /api/v1/teams/{teamId}/inventory/{entryId}
func (h *Handler) GetInventoryEntry(w http.ResponseWriter, r *http.Request) {
	var teamID, entryID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := entryID.Scan(chi.URLParam(r, "entryId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry ID", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.GetInventoryEntry(r.Context(), dbgen.GetInventoryEntryParams{
		ID:     entryID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "inventory entry not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var storLocID *string
	if row.StorageLocationID.Valid {
		s := row.StorageLocationID.String()
		storLocID = &s
	}
	var prefListID *string
	if row.PreferredListingID.Valid {
		s := row.PreferredListingID.String()
		prefListID = &s
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                     row.ID.String(),
		"team_id":                row.TeamID.String(),
		"part_id":                row.PartID.String(),
		"quantity_on_hand":       row.QuantityOnHand,
		"storage_location_id":    storLocID,
		"storage_location_label": row.StorageLocationLabel,
		"preferred_listing_id":   prefListID,
		"minimum_stock_quantity": row.MinimumStockQuantity,
		"reorder_quantity":       row.ReorderQuantity,
	})
}

// UpdateInventoryEntry handles PATCH /api/v1/teams/{teamId}/inventory/{entryId}
func (h *Handler) UpdateInventoryEntry(w http.ResponseWriter, r *http.Request) {
	var teamID, entryID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := entryID.Scan(chi.URLParam(r, "entryId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry ID", "BAD_REQUEST")
		return
	}

	// Decode into a raw map to distinguish absent fields (keep current value)
	// from explicitly-null fields (clear to NULL) from value fields (update).
	var rawReq map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	params := dbgen.UpdateInventoryEntryParams{ID: entryID, TeamID: teamID}

	if raw, ok := rawReq["storage_location_id"]; ok {
		params.SetStorageLocationID = true
		if string(raw) != "null" {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid storage_location_id", "BAD_REQUEST")
				return
			}
			if err := params.StorageLocationID.Scan(s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid storage_location_id", "BAD_REQUEST")
				return
			}
		}
	}

	if raw, ok := rawReq["preferred_listing_id"]; ok {
		params.SetPreferredListingID = true
		if string(raw) != "null" {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid preferred_listing_id", "BAD_REQUEST")
				return
			}
			if err := params.PreferredListingID.Scan(s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid preferred_listing_id", "BAD_REQUEST")
				return
			}
		}
	}

	if raw, ok := rawReq["minimum_stock_quantity"]; ok {
		params.SetMinimumStockQuantity = true
		if string(raw) != "null" {
			var n int32
			if err := json.Unmarshal(raw, &n); err != nil {
				writeError(w, http.StatusBadRequest, "invalid minimum_stock_quantity", "BAD_REQUEST")
				return
			}
			params.MinimumStockQuantity = &n
		}
	}

	if raw, ok := rawReq["reorder_quantity"]; ok {
		params.SetReorderQuantity = true
		if string(raw) != "null" {
			var n int32
			if err := json.Unmarshal(raw, &n); err != nil {
				writeError(w, http.StatusBadRequest, "invalid reorder_quantity", "BAD_REQUEST")
				return
			}
			params.ReorderQuantity = &n
		}
	}

	if raw, ok := rawReq["storage_coordinate"]; ok {
		params.SetStorageCoordinate = true
		if string(raw) != "null" {
			var coords []int32
			if err := json.Unmarshal(raw, &coords); err != nil {
				writeError(w, http.StatusBadRequest, "invalid storage_coordinate", "BAD_REQUEST")
				return
			}
			for _, c := range coords {
				if c < 0 {
					writeError(w, http.StatusBadRequest, "storage_coordinate values must be non-negative", "BAD_REQUEST")
					return
				}
			}
			params.StorageCoordinate = coords
		}
	}

	entry, err := h.Queries.UpdateInventoryEntry(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "inventory entry not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{"part_id": entry.PartID.String(), "entry_id": entry.ID.String()})
	writeJSON(w, http.StatusOK, inventoryEntryResponse(entry))
}

// ListAdjustments handles GET /api/v1/teams/{teamId}/inventory/{entryId}/adjustments
func (h *Handler) ListAdjustments(w http.ResponseWriter, r *http.Request) {
	var teamID, entryID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := entryID.Scan(chi.URLParam(r, "entryId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry ID", "BAD_REQUEST")
		return
	}

	// The adjustment-list queries are keyed by entry id only; confirm the entry
	// belongs to this team before exposing its stock ledger.
	if _, err := h.Queries.GetInventoryEntry(r.Context(), dbgen.GetInventoryEntryParams{
		ID:     entryID,
		TeamID: teamID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "inventory entry not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountAdjustments(r.Context(), entryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListAdjustments(r.Context(), dbgen.ListAdjustmentsParams{
		InventoryEntryID: entryID,
		Offset:           offset,
		Limit:            limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID                   string  `json:"id"`
		Delta                int32   `json:"delta"`
		AdjustedAt           string  `json:"adjusted_at"`
		Reason               *string `json:"reason"`
		CreatedBy            string  `json:"created_by"`
		CreatedByDisplayName string  `json:"created_by_display_name"`
		SourceType           *string `json:"source_type"`
		SourceID             *string `json:"source_id"`
		SourceOrderID        *string `json:"source_order_id"`
	}
	items := make([]item, len(rows))
	for i, row := range rows {
		it := item{
			ID:                   row.ID.String(),
			Delta:                row.Delta,
			AdjustedAt:           row.AdjustedAt.Time.UTC().Format(time.RFC3339),
			Reason:               row.Reason,
			CreatedBy:            row.CreatedBy.String(),
			CreatedByDisplayName: row.CreatedByDisplayName,
		}
		switch {
		case row.OrderLineItemID.Valid:
			st := "order_line_item"
			sid := row.OrderLineItemID.String()
			it.SourceType = &st
			it.SourceID = &sid
			if row.SourceOrderID.Valid {
				soid := row.SourceOrderID.String()
				it.SourceOrderID = &soid
			}
		case row.BuildID.Valid:
			st := "build"
			sid := row.BuildID.String()
			it.SourceType = &st
			it.SourceID = &sid
		case row.GeneralCheckoutID.Valid:
			st := "general_checkout"
			sid := row.GeneralCheckoutID.String()
			it.SourceType = &st
			it.SourceID = &sid
		}
		items[i] = it
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "limit": limit, "offset": offset, "items": items,
	})
}

// CreateAdjustment handles POST /api/v1/teams/{teamId}/inventory/{entryId}/adjustments
func (h *Handler) CreateAdjustment(w http.ResponseWriter, r *http.Request) {
	var teamID, entryID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := entryID.Scan(chi.URLParam(r, "entryId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid entry ID", "BAD_REQUEST")
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "UNAUTHORIZED")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	var req struct {
		Delta  int32   `json:"delta"`
		Reason *string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if req.Delta == 0 {
		writeError(w, http.StatusBadRequest, "delta must be non-zero", "BAD_REQUEST")
		return
	}

	// Resolve the inventory entry so we can include part_id in the SSE event.
	invEntry, err := h.Queries.GetInventoryEntry(r.Context(), dbgen.GetInventoryEntryParams{
		ID:     entryID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "inventory entry not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if req.Delta < 0 && invEntry.QuantityOnHand+req.Delta < 0 {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("adjustment would result in negative stock; current stock is %d, maximum deduction is %d",
				invEntry.QuantityOnHand, invEntry.QuantityOnHand),
			"INSUFFICIENT_STOCK")
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	adj, err := qtx.CreateStockAdjustment(r.Context(), dbgen.CreateStockAdjustmentParams{
		InventoryEntryID: entryID,
		Delta:            req.Delta,
		Reason:           req.Reason,
		CreatedBy:        userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "inventory_changed", teamID.String(), map[string]string{"part_id": invEntry.PartID.String(), "entry_id": entryID.String()})

	// If stock increased, re-evaluate readiness of any ordered builds that need this part.
	if req.Delta > 0 {
		h.reevaluateOrderedBuildsForPart(r, invEntry.PartID, teamID, adj.NewQuantityOnHand)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                 adj.ID.String(),
		"inventory_entry_id": adj.InventoryEntryID.String(),
		"delta":              adj.Delta,
		"adjusted_at":        adj.AdjustedAt.Time.UTC().Format(time.RFC3339),
		"reason":             adj.Reason,
		"created_by":         adj.CreatedBy.String(),
		"quantity_on_hand":   adj.NewQuantityOnHand,
	})
}

// reevaluateOrderedBuildsForPart updates quantity_reserved for ordered builds that need
// the given part (using newly available stock), then transitions any now-ready builds.
// Called after any positive stock adjustment. Runs outside a transaction; best-effort.
func (h *Handler) reevaluateOrderedBuildsForPart(r *http.Request, partID, teamID pgtype.UUID, quantityOnHand int32) {
	ctx := r.Context()

	buildIDs, err := h.Queries.GetOrderedBuildsNeedingPart(ctx, dbgen.GetOrderedBuildsNeedingPartParams{
		PartID: partID,
		TeamID: teamID,
	})
	if err != nil || len(buildIDs) == 0 {
		return
	}

	for _, buildID := range buildIDs {
		allocs, err := h.Queries.GetBuildPartAllocations(ctx, buildID)
		if err != nil {
			continue
		}
		// Find this part's allocation row.
		var alloc *dbgen.GetBuildPartAllocationsRow
		for i := range allocs {
			if allocs[i].PartID == partID {
				alloc = &allocs[i]
				break
			}
		}
		if alloc == nil {
			continue
		}

		// Compute how much free stock is available after accounting for all other builds' reservations.
		totalReserved, err := h.Queries.SumReservedForPart(ctx, dbgen.SumReservedForPartParams{
			PartID: partID,
			TeamID: teamID,
		})
		if err != nil {
			continue
		}
		// freeStock excludes this build's current reservation to avoid double-counting.
		reservedByOthers := totalReserved - alloc.QuantityReserved
		freeStock := quantityOnHand - reservedByOthers
		if freeStock < 0 {
			freeStock = 0
		}

		// How much more stock does this build still need from somewhere?
		stillNeededFromStock := alloc.QuantityNeeded - alloc.QuantityReceived - alloc.QuantityReserved
		if stillNeededFromStock <= 0 {
			continue
		}

		additional := freeStock
		if additional > stillNeededFromStock {
			additional = stillNeededFromStock
		}
		if additional <= 0 {
			continue
		}

		newReservation := alloc.QuantityReserved + additional
		if err := h.Queries.UpdateBuildPartReservation(ctx, dbgen.UpdateBuildPartReservationParams{
			QuantityReserved: newReservation,
			BuildID:          buildID,
			PartID:           partID,
		}); err != nil {
			continue
		}

		isReady, err := h.Queries.CheckBuildReadiness(ctx, buildID)
		if err != nil || !isReady {
			continue
		}
		_ = h.Queries.UpdateBuildStatus(ctx, dbgen.UpdateBuildStatusParams{
			ID:     buildID,
			Status: "ready",
			TeamID: teamID,
		})
		h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": buildID.String()})
	}
}

// ReplenishPreview handles GET /api/v1/teams/{teamId}/inventory/replenish-preview
func (h *Handler) ReplenishPreview(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.ReplenishPreview(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		InventoryEntryID     string  `json:"inventory_entry_id"`
		PartID               string  `json:"part_id"`
		PartMpn              *string `json:"part_mpn"`
		QuantityOnHand       int32   `json:"quantity_on_hand"`
		MinimumStockQuantity *int32  `json:"minimum_stock_quantity"`
		Shortfall            int32   `json:"shortfall"`
		ReorderQuantity      *int32  `json:"reorder_quantity"`
		QuantityToAddToCart  *int32  `json:"quantity_to_add_to_cart"`
		PreferredListingID   *string `json:"preferred_listing_id"`
	}
	items := make([]item, len(rows))
	for i, row := range rows {
		var prefListID *string
		if row.PreferredListingID.Valid {
			s := row.PreferredListingID.String()
			prefListID = &s
		}
		items[i] = item{
			InventoryEntryID:     row.InventoryEntryID.String(),
			PartID:               row.PartID.String(),
			PartMpn:              row.PartMpn,
			QuantityOnHand:       row.QuantityOnHand,
			MinimumStockQuantity: row.MinimumStockQuantity,
			Shortfall:            row.Shortfall,
			ReorderQuantity:      row.ReorderQuantity,
			QuantityToAddToCart:  row.QuantityToAddToCart,
			PreferredListingID:   prefListID,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// inventoryEntryResponse converts an InventoryEntry to a JSON-serializable map.
func inventoryEntryResponse(e dbgen.InventoryEntry) map[string]any {
	var storLocID *string
	if e.StorageLocationID.Valid {
		s := e.StorageLocationID.String()
		storLocID = &s
	}
	var prefListID *string
	if e.PreferredListingID.Valid {
		s := e.PreferredListingID.String()
		prefListID = &s
	}
	return map[string]any{
		"id":                     e.ID.String(),
		"team_id":                e.TeamID.String(),
		"part_id":                e.PartID.String(),
		"quantity_on_hand":       e.QuantityOnHand,
		"storage_location_id":    storLocID,
		"storage_coordinate":     e.StorageCoordinate,
		"preferred_listing_id":   prefListID,
		"minimum_stock_quantity": e.MinimumStockQuantity,
		"reorder_quantity":       e.ReorderQuantity,
	}
}


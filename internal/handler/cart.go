package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// cartListingJSON is the unified listing object returned for every cart item.
// chosen=true means the user explicitly set this listing; false means it was auto-resolved.
type cartListingJSON struct {
	ListingID       string          `json:"listing_id"`
	DistributorID   *string         `json:"distributor_id"`
	DistributorName *string         `json:"distributor_name"`
	SKU             *string         `json:"sku"`
	UnitPrice       *string         `json:"unit_price"`
	FixedOrderCost  *string         `json:"fixed_order_cost"`
	MOQ             int32           `json:"moq"`
	PriceBreaks     json.RawMessage `json:"price_breaks"`
	PackagingType   *string         `json:"packaging_type"`
	StockAvailable  *int32          `json:"stock_available"`
	LastRefreshedAt *string         `json:"last_refreshed_at"`
	OptimizedQty    int32           `json:"optimized_qty"`
	Chosen          bool            `json:"chosen"`
}

// publishCartChanged notifies other team members that the cart has been modified.
func (h *Handler) publishCartChanged(r *http.Request, teamID pgtype.UUID) {
	h.publishEvent(r, "cart_changed", teamID.String(), map[string]string{})
}

func cartItemResponse(i dbgen.GetCartItemsRow) map[string]any {
	m := map[string]any{
		"id":                      i.ID.String(),
		"part_id":                 i.PartID.String(),
		"part_mpn":                i.PartMpn,
		"part_description":        i.PartDescription,
		"part_short_description":  i.PartShortDescription,
		"part_manufacturer":       i.PartManufacturer,
		"part_category":           i.PartCategory,
		"part_package":            i.PartPackage,
		"part_product_url":        i.PartProductUrl,
		"part_datasheet_url":      i.PartDatasheetUrl,
		"part_image_url":          i.PartImageUrl,
		"quantity":                i.Quantity,
		"source":                  string(i.Source),
		"source_bom_line_item_id": uuidPtrString(i.SourceBomLineItemID),
	}
	// For items with an explicit listing, build the unified listing object with chosen=true.
	if i.ListingID.Valid {
		var moq int32
		if i.ListingMoq != nil {
			moq = *i.ListingMoq
		}
		m["listing"] = cartListingJSON{
			ListingID:       i.ListingID.String(),
			DistributorID:   uuidPtrString(i.ListingDistributorID),
			DistributorName: i.ListingDistributorName,
			SKU:             i.ListingSku,
			UnitPrice:       numericString(i.ListingUnitPrice),
			FixedOrderCost:  numericString(i.ListingFixedOrderCost),
			MOQ:             moq,
			PriceBreaks:     json.RawMessage(i.ListingPriceBreaks),
			PackagingType:   i.ListingPackagingType,
			StockAvailable:  i.ListingStockAvailable,
			LastRefreshedAt: timestamptzString(i.ListingLastRefreshedAt),
			OptimizedQty:    optimalOrderQty(i.Quantity, moq, i.ListingPriceBreaks, i.ListingUnitPrice),
			Chosen:          true,
		}
	}
	return m
}

// recomputeBomShortfalls clears all bom_shortfall cart items for the given cart
// and re-inserts them by aggregating demand across all pending builds
// (excluded_from_cart = FALSE), then subtracting current inventory.
// Manual and inventory_maintenance items are not touched.
// q must be the transaction-bound queries object when called inside a transaction.
func (h *Handler) recomputeBomShortfalls(
	ctx context.Context,
	q *dbgen.Queries,
	cartID pgtype.UUID,
	teamID pgtype.UUID,
) (int, error) {
	pendingBuilds, err := q.GetPendingBuildsForTeamCart(ctx, teamID)
	if err != nil {
		return 0, err
	}

	if len(pendingBuilds) == 0 {
		return 0, q.ClearBomShortfallItems(ctx, cartID)
	}

	// Batch-load line item overrides for all pending builds.
	buildIDs := make([]pgtype.UUID, len(pendingBuilds))
	for i, pb := range pendingBuilds {
		buildIDs[i] = pb.ID
	}
	allOverrides, err := q.GetBuildLineItemOverridesForBuilds(ctx, buildIDs)
	if err != nil {
		return 0, err
	}
	// overridesByBuild: build_id → (bom_line_item_id → override_type)
	overridesByBuild := make(map[string]map[string]string, len(pendingBuilds))
	for _, ov := range allOverrides {
		bid := ov.BuildID.String()
		if overridesByBuild[bid] == nil {
			overridesByBuild[bid] = make(map[string]string)
		}
		overridesByBuild[bid][ov.BomLineItemID.String()] = ov.OverrideType
	}

	// Aggregate total demand per part across all included pending builds.
	// Use substitute selections when a build has them locked in.
	demandByPart := make(map[string]int32)
	for _, pb := range pendingBuilds {
		lineItems, err := q.ListBOMLineItems(ctx, pb.BomRevisionID)
		if err != nil {
			return 0, err
		}

		// Load substitute selections for this build (empty if none stored).
		selRows, err := q.GetBuildSubstituteSelections(ctx, pb.ID)
		if err != nil {
			return 0, err
		}
		selectionMap := make(map[string]string, len(selRows))
		for _, s := range selRows {
			selectionMap[s.BomLineItemID.String()] = s.SelectedPartID.String()
		}

		excludedSet := buildExcludedSet(pb.ExcludedDesignators)
		overrideMap := overridesByBuild[pb.ID.String()]

		for _, li := range lineItems {
			if !li.ResolvedPartID.Valid || li.Ignored {
				continue
			}

			// skip/use_available → this build's demand for this part is covered or omitted.
			if ov, ok := overrideMap[li.ID.String()]; ok {
				if ov == "skip" || ov == "use_available" {
					continue
				}
			}

			effectiveQty := li.Quantity
			if len(excludedSet) > 0 && li.ReferenceDesignators != "" {
				if total := totalDesignatorCount(li.ReferenceDesignators); total > 0 {
					included := effectiveDesignatorCount(li.ReferenceDesignators, excludedSet)
					effectiveQty = int32(min(int(li.Quantity), included))
				}
			}
			if effectiveQty <= 0 {
				continue
			}

			partID := li.ResolvedPartID.String()
			if selectedID, ok := selectionMap[li.ID.String()]; ok {
				partID = selectedID
			}
			demandByPart[partID] += effectiveQty * pb.BuildQuantity
		}
	}

	if len(demandByPart) == 0 {
		return 0, q.ClearBomShortfallItems(ctx, cartID)
	}

	// Fetch inventory for all relevant parts in one query.
	partIDs := make([]pgtype.UUID, 0, len(demandByPart))
	for pidStr := range demandByPart {
		var pid pgtype.UUID
		_ = pid.Scan(pidStr)
		partIDs = append(partIDs, pid)
	}

	invRows, err := q.GetInventoryQuantitiesForParts(ctx, dbgen.GetInventoryQuantitiesForPartsParams{
		TeamID:  teamID,
		Column2: partIDs,
	})
	if err != nil {
		return 0, err
	}

	type invEntry struct {
		quantityOnHand       int32
		minimumStockQuantity *int32
		reorderQuantity      *int32
	}
	invByPart := make(map[string]invEntry, len(invRows))
	for _, row := range invRows {
		invByPart[row.PartID.String()] = invEntry{
			quantityOnHand:       row.QuantityOnHand,
			minimumStockQuantity: row.MinimumStockQuantity,
			reorderQuantity:      row.ReorderQuantity,
		}
	}

	// Fetch unallocated inbound on-order quantities so the shortfall reflects only
	// what truly needs to be purchased (excess from existing orders is subtracted).
	unallocatedRows, err := q.GetUnallocatedOnOrderForParts(ctx, dbgen.GetUnallocatedOnOrderForPartsParams{
		PartIds: partIDs,
		TeamID:  teamID,
	})
	if err != nil {
		return 0, err
	}
	unallocatedByPart := make(map[string]int32, len(unallocatedRows))
	for _, row := range unallocatedRows {
		unallocatedByPart[row.PartID.String()] = row.UnallocatedQty
	}

	if err := q.ClearBomShortfallItems(ctx, cartID); err != nil {
		return 0, err
	}

	addedCount := 0
	for pidStr, required := range demandByPart {
		inv := invByPart[pidStr]
		stock := inv.quantityOnHand
		// Net stock = on-hand + unallocated inbound from existing orders.
		netStock := stock + unallocatedByPart[pidStr]
		postBuild := netStock - required

		var cartQty int32
		if inv.minimumStockQuantity != nil && postBuild < *inv.minimumStockQuantity {
			rawQty := *inv.minimumStockQuantity - postBuild
			if inv.reorderQuantity != nil {
				cartQty = *inv.reorderQuantity
			} else {
				cartQty = rawQty
			}
		} else if netStock < required {
			cartQty = required - netStock
		} else {
			continue
		}

		var partID pgtype.UUID
		_ = partID.Scan(pidStr)

		// source_bom_line_item_id is intentionally null: with multiple pending builds
		// a single shortfall item cannot be attributed to one BOM line item.
		if _, err := q.UpsertCartItem(ctx, dbgen.UpsertCartItemParams{
			ShoppingCartID: cartID,
			PartID:         partID,
			Quantity:       cartQty,
			Source:         dbgen.CartItemSourceBomShortfall,
		}); err != nil {
			return addedCount, err
		}
		addedCount++
	}

	return addedCount, nil
}

// GetCartCount handles GET /api/v1/teams/{teamId}/cart/count
func (h *Handler) GetCartCount(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	count, err := h.Queries.CountCartItems(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{"count": count})
}

// GetCart handles GET /api/v1/teams/{teamId}/cart
func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.GetCartItems(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	resolution, err := h.CartResolver.Resolve(r.Context(), teamID.String(), rows, h.Queries)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Compute per-build demand for every part in the cart.
	type buildDemand struct {
		BuildID          string `json:"build_id"`
		ProjectName      string `json:"project_name"`
		BomRevisionLabel string `json:"bom_revision_label"`
		QuantityNeeded   int32  `json:"quantity_needed"`
	}
	buildDemandsByPart := make(map[string][]buildDemand)
	pendingBuilds, err := h.Queries.GetPendingBuildsForTeamCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	for _, pb := range pendingBuilds {
		lineItems, err := h.Queries.ListBOMLineItems(r.Context(), pb.BomRevisionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		for _, li := range lineItems {
			if !li.ResolvedPartID.Valid || li.Ignored {
				continue
			}
			pid := li.ResolvedPartID.String()
			buildDemandsByPart[pid] = append(buildDemandsByPart[pid], buildDemand{
				BuildID:          pb.ID.String(),
				ProjectName:      pb.ProjectName,
				BomRevisionLabel: pb.RevisionLabel,
				QuantityNeeded:   li.Quantity * pb.BuildQuantity,
			})
		}
	}

	items := make([]map[string]any, len(rows))
	for i, row := range rows {
		m := cartItemResponse(row)
		// For items without an explicit listing, populate from the auto-resolved listing.
		if !row.ListingID.Valid {
			if rl := resolution.ByItemID[row.ID.String()]; rl != nil {
				distIDStr := rl.DistributorID.String()
				m["listing"] = cartListingJSON{
					ListingID:       rl.ListingID.String(),
					DistributorID:   &distIDStr,
					DistributorName: &rl.DistributorName,
					SKU:             &rl.SKU,
					UnitPrice:       numericString(rl.UnitPriceAtQty),
					FixedOrderCost:  numericString(rl.FixedOrderCost),
					MOQ:             rl.MOQ,
					PriceBreaks:     json.RawMessage(rl.PriceBreaks),
					PackagingType:   &rl.PackagingType,
					StockAvailable:  rl.StockAvailable,
					LastRefreshedAt: timestamptzString(rl.LastRefreshedAt),
					OptimizedQty:    rl.OptimizedQty,
					Chosen:          false,
				}
			} else {
				m["listing"] = nil
			}
		}
		demands := buildDemandsByPart[row.PartID.String()]
		if demands == nil {
			demands = []buildDemand{}
		}
		m["build_demands"] = demands
		items[i] = m
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

// RefreshCartListings handles POST /api/v1/teams/{teamId}/cart/refresh-listings
// Enqueues a batch listing refresh. For items with an explicit listing, that listing
// is refreshed. For items with no listing, all eligible listings for the part are
// refreshed so auto-resolution has up-to-date data.
// Returns 202 {"job_id": "..."} or 204 if no refreshable listings exist.
func (h *Handler) RefreshCartListings(w http.ResponseWriter, r *http.Request) {
	teamIDStr := chi.URLParam(r, "teamId")
	var teamID pgtype.UUID
	if err := teamID.Scan(teamIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.GetRefreshableCartListings(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if len(rows) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ids := make([]string, len(rows))
	for i, listingID := range rows {
		ids[i] = listingID.String()
	}

	jobID := h.Worker.Enqueue(worker.JobListingRefresh, teamIDStr, worker.ListingRefreshPayload{
		ListingIDs: ids,
		UserID:     userIDFromContext(r),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// ClearCart handles DELETE /api/v1/teams/{teamId}/cart
func (h *Handler) ClearCart(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	if err := h.Queries.ClearCart(r.Context(), teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	w.WriteHeader(http.StatusNoContent)
}

// AddCartItem handles POST /api/v1/teams/{teamId}/cart/items
// Body: {part_id, quantity, listing_id?}
func (h *Handler) AddCartItem(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var body struct {
		PartID    string  `json:"part_id"`
		Quantity  int32   `json:"quantity"`
		ListingID *string `json:"listing_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.PartID == "" {
		writeError(w, http.StatusBadRequest, "part_id is required", "BAD_REQUEST")
		return
	}
	if body.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "quantity must be positive", "BAD_REQUEST")
		return
	}

	var partID pgtype.UUID
	if err := partID.Scan(body.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}
	var listingID pgtype.UUID
	if body.ListingID != nil {
		if err := listingID.Scan(*body.ListingID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid listing_id", "BAD_REQUEST")
			return
		}
	}

	cart, err := h.Queries.GetOrCreateCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	item, err := h.Queries.UpsertCartItem(r.Context(), dbgen.UpsertCartItemParams{
		ShoppingCartID: cart.ID,
		PartID:         partID,
		Quantity:       body.Quantity,
		ListingID:      listingID,
		Source:         dbgen.CartItemSourceManual,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                      item.ID.String(),
		"part_id":                 item.PartID.String(),
		"quantity":                item.Quantity,
		"listing_id":              uuidPtrString(item.ListingID),
		"source":                  string(item.Source),
		"source_bom_line_item_id": uuidPtrString(item.SourceBomLineItemID),
	})
}

// GetCartPartQuantity handles GET /api/v1/teams/{teamId}/cart/part-quantity
// Query params: part_id (required), listing_id (optional)
func (h *Handler) GetCartPartQuantity(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(r.URL.Query().Get("part_id")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}
	var listingID pgtype.UUID
	if s := r.URL.Query().Get("listing_id"); s != "" {
		if err := listingID.Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid listing_id", "BAD_REQUEST")
			return
		}
	}

	qty, err := h.Queries.GetCartQuantityForPart(r.Context(), dbgen.GetCartQuantityForPartParams{
		TeamID:    teamID,
		PartID:    partID,
		ListingID: listingID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int32{"quantity_in_cart": qty})
}

// UpdateCartItem handles PATCH /api/v1/teams/{teamId}/cart/items/{itemId}
// Body: {quantity?, listing_id?}
func (h *Handler) UpdateCartItem(w http.ResponseWriter, r *http.Request) {
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

	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	updated := false

	if v, ok := rawBody["quantity"]; ok {
		var qty int32
		if err := json.Unmarshal(v, &qty); err != nil || qty <= 0 {
			writeError(w, http.StatusBadRequest, "invalid quantity", "BAD_REQUEST")
			return
		}

		// Fetch the item to determine its source and enforce demand floor.
		cartItem, err := h.Queries.GetCartItemByID(r.Context(), dbgen.GetCartItemByIDParams{
			ID:     itemID,
			TeamID: teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "cart item not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		switch cartItem.Source {
		case dbgen.CartItemSourceBomShortfall, dbgen.CartItemSourceInventoryMaintenance:
			// The stored quantity is the demand floor. Users may add manual excess on top.
			floor := cartItem.Quantity
			if qty < floor {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("cannot order less than the required quantity of %d", floor),
					"BELOW_DEMAND_FLOOR")
				return
			}
			excess := qty - floor
			if excess > 0 {
				if _, err := h.Queries.UpsertManualCartItem(r.Context(), dbgen.UpsertManualCartItemParams{
					ShoppingCartID: cartItem.ShoppingCartID,
					PartID:         cartItem.PartID,
					Quantity:       excess,
					ListingID:      cartItem.ListingID,
				}); err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
			} else {
				if err := h.Queries.DeleteManualCartItemForPart(r.Context(), dbgen.DeleteManualCartItemForPartParams{
					ShoppingCartID: cartItem.ShoppingCartID,
					PartID:         cartItem.PartID,
				}); err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
			}
		default: // source = 'manual'
			if _, err := h.Queries.UpdateCartItemQuantity(r.Context(), dbgen.UpdateCartItemQuantityParams{
				ID:       itemID,
				TeamID:   teamID,
				Quantity: qty,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusNotFound, "cart item not found", "NOT_FOUND")
					return
				}
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}
		updated = true
	}

	if v, ok := rawBody["listing_id"]; ok {
		var listingIDStr *string
		if err := json.Unmarshal(v, &listingIDStr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid listing_id", "BAD_REQUEST")
			return
		}
		var listingID pgtype.UUID
		if listingIDStr != nil {
			if err := listingID.Scan(*listingIDStr); err != nil {
				writeError(w, http.StatusBadRequest, "invalid listing_id", "BAD_REQUEST")
				return
			}
		}
		_, err := h.Queries.UpdateCartItemListing(r.Context(), dbgen.UpdateCartItemListingParams{
			ID:        itemID,
			TeamID:    teamID,
			ListingID: listingID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "cart item not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		updated = true
	}

	if !updated {
		writeError(w, http.StatusBadRequest, "no fields to update", "BAD_REQUEST")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	w.WriteHeader(http.StatusNoContent)
}

// RemoveCartItem handles DELETE /api/v1/teams/{teamId}/cart/items/{itemId}
func (h *Handler) RemoveCartItem(w http.ResponseWriter, r *http.Request) {
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

	if err := h.Queries.DeleteCartItem(r.Context(), dbgen.DeleteCartItemParams{
		ID:     itemID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	w.WriteHeader(http.StatusNoContent)
}

// AddLowStock handles POST /api/v1/teams/{teamId}/cart/add-low-stock
// Adds all below-min-stock inventory items to the cart.
func (h *Handler) AddLowStock(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	preview, err := h.Queries.ReplenishPreview(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	cart, err := h.Queries.GetOrCreateCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	addedCount := 0
	for _, row := range preview {
		if row.QuantityToAddToCart == nil {
			continue
		}
		_, err := h.Queries.UpsertCartItem(r.Context(), dbgen.UpsertCartItemParams{
			ShoppingCartID: cart.ID,
			PartID:         row.PartID,
			Quantity:       *row.QuantityToAddToCart,
			ListingID:      row.PreferredListingID,
			Source:         dbgen.CartItemSourceInventoryMaintenance,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		addedCount++
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	writeJSON(w, http.StatusOK, map[string]any{"added_count": addedCount})
}

// ConvertCart handles POST /api/v1/teams/{teamId}/cart/convert
// Converts cart to draft orders grouped by distributor.
// Links orders to pending builds and creates soft reservations.
func (h *Handler) ConvertCart(w http.ResponseWriter, r *http.Request) {
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

	// Fetch items in the format needed by the resolver.
	allItems, err := h.Queries.GetCartItems(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if len(allItems) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "cart is empty", "CART_EMPTY")
		return
	}

	// Auto-resolve listings for items that lack an explicit listing_id.
	resolution, err := h.CartResolver.Resolve(r.Context(), teamID.String(), allItems, h.Queries)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Fetch convert-optimised rows (includes distributor_id and pricing for explicitly-set listings).
	cartItems, err := h.Queries.GetCartItemsForConvert(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Build an effective item list, substituting resolved listing data where listing_id is null.
	type effectiveItem struct {
		partID         pgtype.UUID
		distributorID  pgtype.UUID
		listingID      pgtype.UUID
		quantity       int32
		unitPrice      pgtype.Numeric
		fixedOrderCost pgtype.Numeric
	}
	effectiveItems := make([]effectiveItem, 0, len(cartItems))
	for _, item := range cartItems {
		if item.ListingID.Valid {
			var moq int32
			if item.Moq != nil {
				moq = *item.Moq
			}
			effectiveItems = append(effectiveItems, effectiveItem{
				partID:         item.PartID,
				distributorID:  item.DistributorID,
				listingID:      item.ListingID,
				quantity:       optimalOrderQty(item.Quantity, moq, item.PriceBreaks, item.UnitPrice),
				unitPrice:      item.UnitPrice,
				fixedOrderCost: item.FixedOrderCost,
			})
		} else {
			rl, ok := resolution.ByItemID[item.ID.String()]
			if !ok {
				writeError(w, http.StatusUnprocessableEntity,
					"one or more cart items have no available listing — add a listing or remove those items",
					"MISSING_LISTINGS")
				return
			}
			effectiveItems = append(effectiveItems, effectiveItem{
				partID:         item.PartID,
				distributorID:  rl.DistributorID,
				listingID:      rl.ListingID,
				quantity:       max(item.Quantity, rl.OptimizedQty),
				unitPrice:      rl.UnitPriceAtQty,
				fixedOrderCost: rl.FixedOrderCost,
			})
		}
	}

	// Sort by distributor ID to enable grouping.
	sort.Slice(effectiveItems, func(i, j int) bool {
		return effectiveItems[i].distributorID.String() < effectiveItems[j].distributorID.String()
	})

	// Group consecutive items by distributor.
	type group struct {
		distributorID pgtype.UUID
		items         []effectiveItem
	}
	var groups []group
	for _, ei := range effectiveItems {
		if len(groups) == 0 || groups[len(groups)-1].distributorID.String() != ei.distributorID.String() {
			groups = append(groups, group{distributorID: ei.distributorID})
		}
		groups[len(groups)-1].items = append(groups[len(groups)-1].items, ei)
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	// Track orders created and quantity ordered per part per order.
	type createdOrder struct {
		id            pgtype.UUID
		distributorID pgtype.UUID
		// partID → quantity ordered in this order
		partQty map[string]int32
	}
	var createdOrders []createdOrder

	var orderIDs []string
	for _, g := range groups {
		order, err := qtx.CreateOrder(r.Context(), dbgen.CreateOrderParams{
			TeamID:        teamID,
			DistributorID: g.distributorID,
			CreatedBy:     userID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		co := createdOrder{id: order.ID, distributorID: g.distributorID, partQty: make(map[string]int32)}
		for _, ei := range g.items {
			_, err := qtx.CreateOrderLineItem(r.Context(), dbgen.CreateOrderLineItemParams{
				OrderID:                order.ID,
				ListingID:              ei.listingID,
				QuantityOrdered:        ei.quantity,
				UnitPriceSnapshot:      ei.unitPrice,
				FixedOrderCostSnapshot: ei.fixedOrderCost,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			co.partQty[ei.partID.String()] += ei.quantity
		}
		createdOrders = append(createdOrders, co)
		orderIDs = append(orderIDs, order.ID.String())
	}

	// Clear the cart.
	if err := qtx.ClearCart(r.Context(), teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Process pending builds: link to orders, create allocations, transition status.
	pendingBuilds, err := qtx.GetPendingBuildsForTeamCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Batch-load overrides for all pending builds to use when creating allocations.
	allBuildIDs := make([]pgtype.UUID, len(pendingBuilds))
	for i, pb := range pendingBuilds {
		allBuildIDs[i] = pb.ID
	}
	allOverrides, err := qtx.GetBuildLineItemOverridesForBuilds(r.Context(), allBuildIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	overridesByBuildConvert := make(map[string]map[string]string, len(pendingBuilds))
	for _, ov := range allOverrides {
		bid := ov.BuildID.String()
		if overridesByBuildConvert[bid] == nil {
			overridesByBuildConvert[bid] = make(map[string]string)
		}
		overridesByBuildConvert[bid][ov.BomLineItemID.String()] = ov.OverrideType
	}

	var transitionedBuildIDs []pgtype.UUID

	for _, pb := range pendingBuilds {
		// Get BOM line items for this build.
		lineItems, err := qtx.ListBOMLineItems(r.Context(), pb.BomRevisionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		excludedSet := buildExcludedSet(pb.ExcludedDesignators)
		overrideMap := overridesByBuildConvert[pb.ID.String()]

		// Link build to all created orders.
		for _, co := range createdOrders {
			if err := qtx.CreateBuildOrder(r.Context(), dbgen.CreateBuildOrderParams{
				BuildID: pb.ID,
				OrderID: co.id,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}

		// Compute total quantity ordered per part across newly created orders.
		totalOrderedByPart := make(map[string]int32)
		for _, co := range createdOrders {
			for pidStr, qty := range co.partQty {
				totalOrderedByPart[pidStr] += qty
			}
		}
		// Track which order IDs are already linked to this build (to avoid duplicate inserts).
		linkedOrderIDs := make(map[string]bool)
		for _, co := range createdOrders {
			linkedOrderIDs[co.id.String()] = true
		}

		// For each resolved BOM line item, create an allocation using effective quantities.
		for _, li := range lineItems {
			if !li.ResolvedPartID.Valid || li.Ignored {
				continue
			}

			// skip/use_available overrides: no order needed, no allocation created.
			if ov, ok := overrideMap[li.ID.String()]; ok {
				if ov == "skip" || ov == "use_available" {
					continue
				}
			}

			// Apply designator exclusions to get effective quantity.
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
			quantityNeeded := effectivePerBoard * pb.BuildQuantity

			// Start with what the new orders cover.
			quantityFromOrder := totalOrderedByPart[partIDStr]
			if quantityFromOrder > quantityNeeded {
				quantityFromOrder = quantityNeeded
			}

			// If new orders don't cover the full need, look for unallocated excess in existing orders.
			if quantityFromOrder < quantityNeeded {
				stillNeeded := quantityNeeded - quantityFromOrder
				existingOrders, err := qtx.GetOrdersWithQtyForPart(r.Context(), dbgen.GetOrdersWithQtyForPartParams{
					PartID: li.ResolvedPartID,
					TeamID: teamID,
				})
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
				for _, eo := range existingOrders {
					if stillNeeded <= 0 {
						break
					}
					if linkedOrderIDs[eo.OrderID.String()] {
						continue // already linked via new order
					}
					committed, err := qtx.GetCommittedQtyForOrderPart(r.Context(), dbgen.GetCommittedQtyForOrderPartParams{
						OrderID: eo.OrderID,
						PartID:  li.ResolvedPartID,
						TeamID:  teamID,
					})
					if err != nil {
						writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
						return
					}
					unallocated := eo.OutstandingQty - committed
					if unallocated <= 0 {
						continue
					}
					// Link this build to the existing order.
					if err := qtx.CreateBuildOrder(r.Context(), dbgen.CreateBuildOrderParams{
						BuildID: pb.ID,
						OrderID: eo.OrderID,
					}); err != nil {
						writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
						return
					}
					linkedOrderIDs[eo.OrderID.String()] = true
					absorb := unallocated
					if absorb > stillNeeded {
						absorb = stillNeeded
					}
					quantityFromOrder += absorb
					stillNeeded -= absorb
				}
				if quantityFromOrder > quantityNeeded {
					quantityFromOrder = quantityNeeded
				}
			}

			quantityFromStock := quantityNeeded - quantityFromOrder
			if quantityFromStock < 0 {
				quantityFromStock = 0
			}

			// Clamp quantity_from_stock by how much free stock is actually available.
			if quantityFromStock > 0 {
				alreadyReserved, err := qtx.SumReservedForPart(r.Context(), dbgen.SumReservedForPartParams{
					PartID: li.ResolvedPartID,
					TeamID: teamID,
				})
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
				invEntry, err := qtx.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
					TeamID: teamID,
					PartID: li.ResolvedPartID,
				})
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
				var quantityOnHand int32
				if err == nil {
					quantityOnHand = invEntry.QuantityOnHand
				}
				quantityFree := quantityOnHand - alreadyReserved
				if quantityFree < 0 {
					quantityFree = 0
				}
				if quantityFromStock > quantityFree {
					quantityFromStock = quantityFree
				}
			}

			if _, err := qtx.UpsertBuildPartAllocation(r.Context(), dbgen.UpsertBuildPartAllocationParams{
				BuildID:          pb.ID,
				PartID:           li.ResolvedPartID,
				QuantityNeeded:   quantityNeeded,
				QuantityReserved: quantityFromStock,
				QuantityReceived: 0,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
		}

		// Check readiness: if all parts covered by reservation alone, transition to ready.
		isReady, err := qtx.CheckBuildReadiness(r.Context(), pb.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		newStatus := "ordered"
		if isReady {
			newStatus = "ready"
		}

		if err := qtx.UpdateBuildStatus(r.Context(), dbgen.UpdateBuildStatusParams{
			ID:     pb.ID,
			Status: newStatus,
			TeamID: teamID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}

		transitionedBuildIDs = append(transitionedBuildIDs, pb.ID)
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{})
	for _, bid := range transitionedBuildIDs {
		h.publishEvent(r, "build_changed", teamID.String(), map[string]string{"build_id": bid.String()})
	}

	if orderIDs == nil {
		orderIDs = []string{}
	}
	h.record(r, activity.Event{
		TeamID:  teamID,
		Action:  "cart.converted",
		Feed:    activity.FeedTeam,
		Values:  map[string]any{"order_count": len(orderIDs)},
		Summary: fmt.Sprintf("Converted the cart into %d order(s)", len(orderIDs)),
	})
	writeJSON(w, http.StatusOK, map[string]any{"order_ids": orderIDs})
}

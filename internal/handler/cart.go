package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// publishCartChanged notifies other team members that the cart has been modified.
func (h *Handler) publishCartChanged(r *http.Request, teamID pgtype.UUID) {
	h.publishEvent(r, "cart_changed", teamID.String(), map[string]string{})
}

func cartItemResponse(i dbgen.GetCartItemsRow) map[string]any {
	return map[string]any{
		"id":                       i.ID.String(),
		"part_id":                  i.PartID.String(),
		"part_mpn":                 i.PartMpn,
		"quantity":                 i.Quantity,
		"listing_id":               uuidPtrString(i.ListingID),
		"listing_sku":              i.ListingSku,
		"listing_distributor_name": i.ListingDistributorName,
		"listing_distributor_id":   uuidPtrString(i.ListingDistributorID),
		"unit_price":               numericString(i.ListingUnitPrice),
		"price_breaks":             json.RawMessage(i.ListingPriceBreaks),
		"fixed_order_cost":         numericString(i.ListingFixedOrderCost),
		"source":                   string(i.Source),
		"source_bom_line_item_id":  uuidPtrString(i.SourceBomLineItemID),
	}
}

// GetCart handles GET /api/v1/teams/{teamId}/cart
func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	if _, err := h.Queries.GetOrCreateCart(r.Context(), teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
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

	items := make([]map[string]any, len(rows))
	for i, row := range rows {
		m := cartItemResponse(row)
		var rl *resolvedListing
		if !row.ListingID.Valid {
			rl = resolution.ByItemID[row.ID.String()]
		}
		if rl != nil {
			m["resolved_listing"] = map[string]any{
				"listing_id":       rl.ListingID.String(),
				"distributor_id":   rl.DistributorID.String(),
				"distributor_name": rl.DistributorName,
				"sku":              rl.SKU,
				"unit_price":       numericString(rl.UnitPriceAtQty),
				"fixed_order_cost": numericString(rl.FixedOrderCost),
				"moq":              rl.MOQ,
			}
		} else {
			m["resolved_listing"] = nil
		}
		items[i] = m
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
		_, err := h.Queries.UpdateCartItemQuantity(r.Context(), dbgen.UpdateCartItemQuantityParams{
			ID:       itemID,
			TeamID:   teamID,
			Quantity: qty,
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

// AddShortfalls handles POST /api/v1/teams/{teamId}/cart/add-shortfall
// Body: {bom_revision_id, build_quantity}. Merges BOM shortfalls and restock gaps into the cart.
func (h *Handler) AddShortfalls(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var body struct {
		BomRevisionID string `json:"bom_revision_id"`
		BuildQuantity int32  `json:"build_quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.BomRevisionID == "" {
		writeError(w, http.StatusBadRequest, "bom_revision_id is required", "BAD_REQUEST")
		return
	}
	if body.BuildQuantity <= 0 {
		writeError(w, http.StatusBadRequest, "build_quantity must be positive", "BAD_REQUEST")
		return
	}

	var revID pgtype.UUID
	if err := revID.Scan(body.BomRevisionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid bom_revision_id", "BAD_REQUEST")
		return
	}

	type liResult struct {
		items []dbgen.ListBOMLineItemsRow
		err   error
	}
	type invResult struct {
		rows []dbgen.GetInventoryQuantitiesForRevisionRow
		err  error
	}
	liCh := make(chan liResult, 1)
	invCh := make(chan invResult, 1)
	go func() {
		items, err := h.Queries.ListBOMLineItems(r.Context(), revID)
		liCh <- liResult{items, err}
	}()
	go func() {
		rows, err := h.Queries.GetInventoryQuantitiesForRevision(r.Context(),
			dbgen.GetInventoryQuantitiesForRevisionParams{
				BomRevisionID: revID,
				TeamID:        teamID,
			})
		invCh <- invResult{rows, err}
	}()
	liRes := <-liCh
	invRes := <-invCh
	if liRes.err != nil || invRes.err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	lineItems := liRes.items

	invByPart := make(map[string]dbgen.GetInventoryQuantitiesForRevisionRow)
	for _, row := range invRes.rows {
		invByPart[row.PartID.String()] = row
	}

	cart, err := h.Queries.GetOrCreateCart(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	addedCount := 0
	for _, li := range lineItems {
		if !li.ResolvedPartID.Valid {
			continue
		}
		required := li.Quantity * body.BuildQuantity
		inv := invByPart[li.ResolvedPartID.String()]
		stock := inv.QuantityOnHand
		postBuild := stock - required

		var cartQty int32
		if inv.MinimumStockQuantity != nil && postBuild < *inv.MinimumStockQuantity {
			// Post-build stock would be below minimum — top up to min_stock_qty.
			rawQty := *inv.MinimumStockQuantity - postBuild
			if inv.ReorderQuantity != nil {
				cartQty = *inv.ReorderQuantity
			} else {
				cartQty = rawQty
			}
		} else if stock < required {
			cartQty = required - stock
		} else {
			continue
		}

		_, err := h.Queries.UpsertCartItem(r.Context(), dbgen.UpsertCartItemParams{
			ShoppingCartID:      cart.ID,
			PartID:              li.ResolvedPartID,
			Quantity:            cartQty,
			Source:              dbgen.CartItemSourceBomShortfall,
			SourceBomLineItemID: li.ID,
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
		distributorID  pgtype.UUID
		listingID      pgtype.UUID
		quantity       int32
		unitPrice      pgtype.Numeric
		fixedOrderCost pgtype.Numeric
	}
	effectiveItems := make([]effectiveItem, 0, len(cartItems))
	for _, item := range cartItems {
		if item.ListingID.Valid {
			effectiveItems = append(effectiveItems, effectiveItem{
				distributorID:  item.DistributorID,
				listingID:      item.ListingID,
				quantity:       item.Quantity,
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
				distributorID:  rl.DistributorID,
				listingID:      rl.ListingID,
				quantity:       item.Quantity,
				unitPrice:      rl.UnitPriceAtQty,
				fixedOrderCost: rl.FixedOrderCost,
			})
		}
	}

	// Sort by distributor ID to enable grouping (auto-resolved items may have different
	// distributor IDs than the DB ordering assumed).
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
		}
		orderIDs = append(orderIDs, order.ID.String())
	}

	if err := qtx.ClearCart(r.Context(), teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)
	h.publishEvent(r, "order_changed", teamID.String(), map[string]string{})

	if orderIDs == nil {
		orderIDs = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"order_ids": orderIDs})
}

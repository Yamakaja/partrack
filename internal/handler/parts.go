package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
)

// ListParts handles GET /api/v1/parts
// Query: team_id?, q?, category?, package?, mpn?, lifecycle_status?, scope?, stock_status?, limit, offset
func (h *Handler) ListParts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := parsePaginationParams(r)

	params := dbgen.ListPartsWithInventoryParams{Limit: limit, Offset: offset}
	countParams := dbgen.CountPartsWithInventoryParams{}

	if v := q.Get("team_id"); v != "" {
		var teamID pgtype.UUID
		if err := teamID.Scan(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
			return
		}
		if !h.requireTeamMembership(w, r, teamID) {
			return
		}
		params.TeamID = teamID
		countParams.TeamID = teamID
	}
	if v := q.Get("q"); v != "" {
		params.Q = &v
		countParams.Q = &v
	}
	if v := q.Get("mpn"); v != "" {
		params.MpnExact = &v
		countParams.MpnExact = &v
	}
	if v := q.Get("category_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			n32 := int32(n)
			params.CategoryID = &n32
			countParams.CategoryID = &n32
		}
	}
	if v := q.Get("package"); v != "" {
		params.Package = &v
		countParams.Package = &v
	}
	if v := q.Get("lifecycle_status"); v != "" {
		params.LifecycleStatus = &v
		countParams.LifecycleStatus = &v
	}
	if v := q.Get("scope"); v != "" {
		params.FilterScope = dbgen.NullPartScope{PartScope: dbgen.PartScope(v), Valid: true}
		countParams.FilterScope = params.FilterScope
	}
	if v := q.Get("stock_status"); v != "" {
		params.StockStatus = &v
		countParams.StockStatus = &v
	}
	if v := q.Get("storage_location_id"); v != "" {
		var uid pgtype.UUID
		if err := uid.Scan(v); err == nil {
			params.StorageLocationID = uid
			countParams.StorageLocationID = uid
		}
	}
	if v := q.Get("storage_cells"); v != "" {
		params.StorageCells = &v
		countParams.StorageCells = &v
	}

	// Parameter filters: param_{id}_min, param_{id}_max (numeric) and param_{id}_values (enum/text)
	type paramFilter struct {
		isNumeric bool
		min, max  *float64 // nil = no bound
		values    []string
	}
	paramFilters := map[int32]*paramFilter{}
	for key, vals := range q {
		if len(vals) == 0 || !strings.HasPrefix(key, "param_") {
			continue
		}
		rest := key[len("param_"):]
		var paramID int32
		var suffix string
		if idx := strings.LastIndex(rest, "_"); idx > 0 {
			n, err := strconv.Atoi(rest[:idx])
			if err != nil {
				continue
			}
			paramID = int32(n)
			suffix = rest[idx+1:]
		} else {
			continue
		}
		f := paramFilters[paramID]
		if f == nil {
			f = &paramFilter{}
			paramFilters[paramID] = f
		}
		switch suffix {
		case "min":
			if v, err := strconv.ParseFloat(vals[0], 64); err == nil {
				f.isNumeric = true
				f.min = &v
			}
		case "max":
			if v, err := strconv.ParseFloat(vals[0], 64); err == nil {
				f.isNumeric = true
				f.max = &v
			}
		case "values":
			f.values = vals // repeated query params: param_{id}_values=A&param_{id}_values=B
		}
	}

	if len(paramFilters) > 0 {
		teamID := params.TeamID
		// Fan out all parameter filter queries in parallel, then intersect results.
		type indexedSet struct {
			idx int
			set map[string]struct{}
		}
		results := make([]indexedSet, len(paramFilters))
		eg, egCtx := errgroup.WithContext(r.Context())
		i := 0
		for paramID, f := range paramFilters {
			idx := i
			pid := paramID
			f := f
			i++
			eg.Go(func() error {
				var uuids []pgtype.UUID
				var queryErr error
				if f.isNumeric {
					uuids, queryErr = h.Queries.FindPartIDsByParameterRange(egCtx, dbgen.FindPartIDsByParameterRangeParams{
						ParameterID: &pid,
						MinVal:      f.min,
						MaxVal:      f.max,
						TeamID:      teamID,
					})
				} else if len(f.values) > 0 {
					uuids, queryErr = h.Queries.FindPartIDsByParameterValues(egCtx, dbgen.FindPartIDsByParameterValuesParams{
						ParameterID: &pid,
						Column2:     f.values,
						TeamID:      teamID,
					})
				}
				if queryErr != nil {
					return queryErr
				}
				set := make(map[string]struct{}, len(uuids))
				for _, u := range uuids {
					set[u.String()] = struct{}{}
				}
				results[idx] = indexedSet{idx: idx, set: set}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		var idSets []map[string]struct{}
		for _, res := range results {
			idSets = append(idSets, res.set)
		}

		// Intersect all sets
		var intersection map[string]struct{}
		for i, set := range idSets {
			if i == 0 {
				intersection = set
				continue
			}
			merged := make(map[string]struct{})
			for id := range set {
				if _, ok := intersection[id]; ok {
					merged[id] = struct{}{}
				}
			}
			intersection = merged
		}

		if len(intersection) == 0 {
			// No parts match the parameter filters; return empty result immediately
			writeJSON(w, http.StatusOK, map[string]any{
				"total": int64(0), "limit": limit, "offset": offset, "items": []any{},
			})
			return
		}

		// Build comma-joined UUID string for the SQL filter
		ids := make([]string, 0, len(intersection))
		for id := range intersection {
			ids = append(ids, id)
		}
		joined := strings.Join(ids, ",")
		params.PartIDFilter = &joined
		countParams.PartIDFilter = &joined
	}

	total, err := h.Queries.CountPartsWithInventory(r.Context(), countParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	parts, err := h.Queries.ListPartsWithInventory(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID                   string         `json:"id"`
		Scope                string         `json:"scope"`
		Mpn                  *string        `json:"mpn"`
		Manufacturer         *string        `json:"manufacturer"`
		Description          *string        `json:"description"`
		ShortDescription     *string        `json:"short_description"`
		Category             *string        `json:"category"`
		CategoryID           *int32         `json:"category_id"`
		CategoryName         *string        `json:"category_name"`
		CategorySource       string         `json:"category_source"`
		Package              *string        `json:"package"`
		LifecycleStatus      string         `json:"lifecycle_status"`
		InventoryEntryID     *string        `json:"inventory_entry_id"`
		QuantityOnHand       *int32         `json:"quantity_on_hand"`
		MinimumStockQuantity *int32         `json:"minimum_stock_quantity"`
		ReorderQuantity      *int32         `json:"reorder_quantity"`
		StorageLocationLabel *string        `json:"storage_location_label"`
		Parameters           map[string]any `json:"parameters,omitempty"`
	}
	items := make([]item, len(parts))
	for i, p := range parts {
		it := item{
			ID:                   p.ID.String(),
			Scope:                string(p.Scope),
			Mpn:                  p.Mpn,
			Manufacturer:         p.Manufacturer,
			Description:          p.Description,
			ShortDescription:     p.ShortDescription,
			Category:             p.Category,
			CategoryID:           p.CategoryID,
			CategoryName:         p.CategoryName,
			CategorySource:       p.CategorySource,
			Package:              p.Package,
			LifecycleStatus:      string(p.LifecycleStatus),
			QuantityOnHand:       p.QuantityOnHand,
			MinimumStockQuantity: p.MinimumStockQuantity,
			ReorderQuantity:      p.ReorderQuantity,
			StorageLocationLabel: p.StorageLocationLabel,
		}
		if p.InventoryEntryID.Valid {
			s := p.InventoryEntryID.String()
			it.InventoryEntryID = &s
		}
		items[i] = it
	}

	// When a category filter is active, fetch parameter values for the current page.
	if params.CategoryID != nil && len(parts) > 0 {
		partUUIDs := make([]pgtype.UUID, len(parts))
		for i, p := range parts {
			partUUIDs[i] = p.ID
		}
		paramRows, err := h.Queries.GetParametersByPartIDs(r.Context(), partUUIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		// Build map: partID → slug → value
		paramMap := make(map[string]map[string]any, len(parts))
		for _, row := range paramRows {
			partID := row.PartID.String()
			if _, ok := paramMap[partID]; !ok {
				paramMap[partID] = make(map[string]any)
			}
			if row.NumericValue != nil && row.NumericValueMax != nil && *row.NumericValue != *row.NumericValueMax {
				paramMap[partID][row.Slug] = map[string]any{"min": *row.NumericValue, "max": *row.NumericValueMax}
			} else if row.NumericValue != nil {
				paramMap[partID][row.Slug] = *row.NumericValue
			} else if row.Value != "" {
				paramMap[partID][row.Slug] = row.Value
			}
		}
		for i := range items {
			if pm, ok := paramMap[items[i].ID]; ok {
				items[i].Parameters = pm
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "limit": limit, "offset": offset, "items": items,
	})
}

// CreatePart handles POST /api/v1/parts
func (h *Handler) CreatePart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope           string  `json:"scope"`
		TeamID          *string `json:"team_id"`
		Mpn             *string `json:"mpn"`
		Manufacturer    *string `json:"manufacturer"`
		Description     *string `json:"description"`
		Category        *string `json:"category"`
		CategoryID      *int32  `json:"category_id"`
		Package         *string `json:"package"`
		DatasheetURL    *string `json:"datasheet_url"`
		LifecycleStatus string  `json:"lifecycle_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Scope == "" {
		writeError(w, http.StatusBadRequest, "scope is required", "BAD_REQUEST")
		return
	}

	var teamID pgtype.UUID
	switch req.Scope {
	case "global":
		if req.TeamID != nil {
			writeError(w, http.StatusBadRequest, "team_id must not be set for global parts", "BAD_REQUEST")
			return
		}
		user := middleware.UserFromContext(r.Context())
		if !user.IsAdmin {
			writeError(w, http.StatusForbidden, "creating global parts requires admin", "FORBIDDEN")
			return
		}
	case "team":
		if req.TeamID == nil {
			writeError(w, http.StatusBadRequest, "team_id is required for team-scoped parts", "BAD_REQUEST")
			return
		}
		if err := teamID.Scan(*req.TeamID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
			return
		}
		user := middleware.UserFromContext(r.Context())
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if _, err := h.Queries.CheckTeamMembership(r.Context(), dbgen.CheckTeamMembershipParams{
			TeamID: teamID,
			UserID: userID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "scope must be 'global' or 'team'", "BAD_REQUEST")
		return
	}

	lifecycleStatus := dbgen.PartLifecycleStatusActive
	if req.LifecycleStatus != "" {
		lifecycleStatus = dbgen.PartLifecycleStatus(req.LifecycleStatus)
	}

	// Prevent duplicate parts: if MPN + manufacturer are both provided, check for an existing match.
	if req.Mpn != nil && *req.Mpn != "" && req.Manufacturer != nil && *req.Manufacturer != "" {
		matched, err := h.Queries.ListPartsByMPNAndManufacturer(r.Context(), dbgen.ListPartsByMPNAndManufacturerParams{
			Mpn:          req.Mpn,
			Manufacturer: req.Manufacturer,
			TeamID:       teamID,
		})
		if err == nil && len(matched) > 0 {
			existing := matched[0]
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "A part with this MPN and manufacturer already exists",
				"code":  "PART_EXISTS",
				"existing_part": map[string]any{
					"id":           existing.ID.String(),
					"mpn":          existing.Mpn,
					"manufacturer": existing.Manufacturer,
					"description":  existing.Description,
				},
			})
			return
		}
	}

	var createdBy pgtype.UUID
	if u := middleware.UserFromContext(r.Context()); u != nil {
		_ = createdBy.Scan(u.ID)
	}

	part, err := h.Queries.CreatePart(r.Context(), dbgen.CreatePartParams{
		Scope:           dbgen.PartScope(req.Scope),
		TeamID:          teamID,
		Mpn:             req.Mpn,
		Manufacturer:    req.Manufacturer,
		Description:     req.Description,
		Category:        req.Category,
		CategoryID:      req.CategoryID,
		Package:         req.Package,
		DatasheetUrl:    req.DatasheetURL,
		LifecycleStatus: lifecycleStatus,
		CreatedBy:       createdBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if part.TeamID.Valid {
		h.publishEvent(r, "part_changed", part.TeamID.String(), map[string]string{"part_id": part.ID.String()})
		h.record(r, activity.Event{
			TeamID:  part.TeamID,
			Action:  "part.created",
			Feed:    activity.FeedTeam,
			Subject: &activity.Ref{Type: "part", ID: part.ID.String(), Label: derefStr(part.Mpn)},
			Summary: "Created part " + derefStr(part.Mpn),
		})
	}
	w.Header().Set("Location", "/api/v1/parts/"+part.ID.String())
	writeJSON(w, http.StatusCreated, toPartFullResponse(part))
}

// GetPart handles GET /api/v1/parts/{partId}
// Query: team_id? — when provided, inventory data for the team is included in the response.
func (h *Handler) GetPart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	// Inventory fields, populated when team_id is present.
	type storageLocationObj struct {
		ID           string          `json:"id"`
		Label        string          `json:"label"`
		AncestorPath *string         `json:"ancestor_path"`
		GridConfig   json.RawMessage `json:"grid_config"`
		Coordinate   []int32         `json:"coordinate"`
	}

	var (
		inventoryEntryID     *string
		quantityOnHand       *int32
		minimumStockQuantity *int32
		reorderQuantity      *int32
		preferredListingID   *string
		storageLocation      *storageLocationObj
		categoryName      *string
		categoryParentID  *int32
	)

	teamIDStr := r.URL.Query().Get("team_id")
	var partBase dbgen.Part
	if teamIDStr != "" {
		var teamID pgtype.UUID
		if err := teamID.Scan(teamIDStr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
			return
		}
		if !h.requireTeamMembership(w, r, teamID) {
			return
		}
		row, err := h.Queries.GetPartWithInventory(r.Context(), dbgen.GetPartWithInventoryParams{
			ID: partID, TeamID: teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		// Reconstruct a Part for the shared helper.
		partBase = dbgen.Part{
			ID: row.ID, Scope: row.Scope, TeamID: row.TeamID,
			Mpn: row.Mpn, Manufacturer: row.Manufacturer, Description: row.Description,
			ShortDescription: row.ShortDescription,
			Category:         row.Category, CategoryID: row.CategoryID, CategorySource: row.CategorySource,
			Package: row.Package, DatasheetUrl: row.DatasheetUrl,
			ProductUrl: row.ProductUrl, ImageUrl: row.ImageUrl,
			LifecycleStatus:    row.LifecycleStatus,
			EnrichmentSourceID: row.EnrichmentSourceID, EnrichedAt: row.EnrichedAt,
			PreferredParameterSourceID: row.PreferredParameterSourceID,
			CreatedAt:                  row.CreatedAt,
		}
		categoryName = row.CategoryName
		categoryParentID = row.CategoryParentID
		if row.InventoryEntryID.Valid {
			s := row.InventoryEntryID.String()
			inventoryEntryID = &s
		}
		quantityOnHand = row.QuantityOnHand
		minimumStockQuantity = row.MinimumStockQuantity
		reorderQuantity = row.ReorderQuantity
		if row.PreferredListingID.Valid {
			s := row.PreferredListingID.String()
			preferredListingID = &s
		}
		if row.StorageLocationID.Valid {
			label := ""
			if row.StorageLocationLabel != nil {
				label = *row.StorageLocationLabel
			}
			sl := &storageLocationObj{
				ID:         row.StorageLocationID.String(),
				Label:      label,
				GridConfig: json.RawMessage(row.StorageLocationGridConfig),
			}
			// Build ancestor path from parent chain (if this location has a parent).
			if row.StorageLocationParentID.Valid {
				ancestors, err := h.Queries.ListStorageLocationAncestors(r.Context(), dbgen.ListStorageLocationAncestorsParams{
					LocationID: row.StorageLocationID,
					TeamID:     teamID,
				})
				if err == nil && len(ancestors) > 1 {
					// ancestors is ordered root-first; last element is the location itself.
					labels := make([]string, len(ancestors)-1)
					for i, a := range ancestors[:len(ancestors)-1] {
						labels[i] = a.Label
					}
					path := strings.Join(labels, " > ")
					sl.AncestorPath = &path
				}
			}
			sl.Coordinate = row.StorageCoordinate
			storageLocation = sl
		}
	} else {
		var err error
		partBase, err = h.Queries.GetPart(r.Context(), partID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	params, err := h.Queries.ListParameters(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	listings, err := h.Queries.ListListings(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	aliasRows, err := h.Queries.GetPartAliases(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	aliases := make([]string, len(aliasRows))
	copy(aliases, aliasRows)

	type paramBase struct {
		ID                   string   `json:"id"`
		Name                 string   `json:"name"`
		Value                string   `json:"value"`
		Unit                 *string  `json:"unit"`
		ParameterID          *int32   `json:"parameter_id"`
		ParameterSlug        *string  `json:"parameter_slug"`
		ParameterDisplayName *string  `json:"parameter_display_name"`
		ParameterUnitSymbol  *string  `json:"parameter_unit_symbol"`
		NumericValue         *float64 `json:"numeric_value"`
		NumericValueMax      *float64 `json:"numeric_value_max"`
	}
	type userParam struct {
		paramBase
	}
	type distParam struct {
		paramBase
		SourceDistributorID   string  `json:"source_distributor_id"`
		SourceDistributorName *string `json:"source_distributor_name"`
	}
	userParams := []userParam{}
	distParams := []distParam{}
	for _, p := range params {
		base := paramBase{
			ID:                   p.ID.String(),
			Name:                 p.Name,
			Value:                p.Value,
			Unit:                 p.Unit,
			ParameterID:          p.ParameterID,
			ParameterSlug:        p.ParameterSlug,
			ParameterDisplayName: p.ParameterDisplayName,
			ParameterUnitSymbol:  p.ParameterUnitSymbol,
			NumericValue:         p.NumericValue,
			NumericValueMax:      p.NumericValueMax,
		}
		if p.SourceType == dbgen.ParameterSourceTypeUser {
			userParams = append(userParams, userParam{paramBase: base})
		} else {
			distParams = append(distParams, distParam{
				paramBase:             base,
				SourceDistributorID:   p.SourceDistributorID.String(),
				SourceDistributorName: p.SourceDistributorName,
			})
		}
	}

	type listingItem struct {
		ID              string          `json:"id"`
		DistributorID   string          `json:"distributor_id"`
		DistributorName string          `json:"distributor_name"`
		Sku             string          `json:"sku"`
		PackagingType   string          `json:"packaging_type"`
		UnitPrice       *string         `json:"unit_price"`
		StockAvailable  *int32          `json:"stock_available"`
		Moq             int32           `json:"moq"`
		FixedOrderCost  *string         `json:"fixed_order_cost"`
		PriceBreaks     json.RawMessage `json:"price_breaks"`
		LastRefreshedAt *string         `json:"last_refreshed_at"`
	}
	listingItems := make([]listingItem, len(listings))
	for i, l := range listings {
		var lastRef *string
		if l.LastRefreshedAt.Valid {
			s := l.LastRefreshedAt.Time.UTC().Format(time.RFC3339)
			lastRef = &s
		}
		listingItems[i] = listingItem{
			ID:              l.ID.String(),
			DistributorID:   l.DistributorID.String(),
			DistributorName: l.DistributorName,
			Sku:             l.Sku,
			PackagingType:   l.PackagingType,
			UnitPrice:       numericString(l.UnitPrice),
			StockAvailable:  l.StockAvailable,
			Moq:             l.Moq,
			FixedOrderCost:  numericString(l.FixedOrderCost),
			PriceBreaks:     json.RawMessage(l.PriceBreaks),
			LastRefreshedAt: lastRef,
		}
	}

	base := toPartFullResponse(partBase)
	base.CategoryName = categoryName
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                            base.ID,
		"scope":                         base.Scope,
		"team_id":                       base.TeamID,
		"mpn":                           base.Mpn,
		"manufacturer":                  base.Manufacturer,
		"description":                   base.Description,
		"short_description":             base.ShortDescription,
		"category":                      base.Category,
		"category_id":                   base.CategoryID,
		"category_name":                 base.CategoryName,
		"category_source":               base.CategorySource,
		"category_parent_id":            categoryParentID,
		"package":                       base.Package,
		"datasheet_url":                 base.DatasheetURL,
		"product_url":                   base.ProductURL,
		"image_url":                     base.ImageURL,
		"lifecycle_status":              base.LifecycleStatus,
		"enrichment_source_id":          base.EnrichmentSourceID,
		"enriched_at":                   base.EnrichedAt,
		"preferred_parameter_source_id": base.PreferredParameterSourceID,
		"created_at":                    base.CreatedAt,
		"aliases":                       aliases,
		"inventory_entry_id":            inventoryEntryID,
		"quantity_on_hand":              quantityOnHand,
		"minimum_stock_quantity":        minimumStockQuantity,
		"reorder_quantity":              reorderQuantity,
		"preferred_listing_id":          preferredListingID,
		"storage_location":              storageLocation,
		"parameters": map[string]any{
			"user_specified":      userParams,
			"distributor_sourced": distParams,
		},
		"listings": listingItems,
	})
}

// UpdatePart handles PATCH /api/v1/parts/{partId}
func (h *Handler) UpdatePart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	// For team-scoped parts, verify the caller is a member of the owning team.
	existing, err := h.Queries.GetPart(r.Context(), partID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if existing.Scope == dbgen.PartScopeTeam {
		user := middleware.UserFromContext(r.Context())
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if _, err := h.Queries.CheckTeamMembership(r.Context(), dbgen.CheckTeamMembershipParams{
			TeamID: existing.TeamID,
			UserID: userID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	var req struct {
		Description                *string `json:"description"`
		Category                   *string `json:"category"`
		CategoryID                 *int32  `json:"category_id"`
		Package                    *string `json:"package"`
		DatasheetURL               *string `json:"datasheet_url"`
		LifecycleStatus            *string `json:"lifecycle_status"`
		PreferredParameterSourceID *string `json:"preferred_parameter_source_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	params := dbgen.UpdatePartParams{ID: partID}
	params.Description = req.Description
	params.Category = req.Category
	params.CategoryID = req.CategoryID
	params.Package = req.Package
	params.DatasheetUrl = req.DatasheetURL
	if req.LifecycleStatus != nil {
		params.LifecycleStatus = dbgen.NullPartLifecycleStatus{
			PartLifecycleStatus: dbgen.PartLifecycleStatus(*req.LifecycleStatus),
			Valid:               true,
		}
	}
	if req.PreferredParameterSourceID != nil {
		if err := params.PreferredParameterSourceID.Scan(*req.PreferredParameterSourceID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid preferred_parameter_source_id", "BAD_REQUEST")
			return
		}
	}

	part, err := h.Queries.UpdatePart(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	// Broadcast to the owning team (team-scoped), or to a team_id provided by the caller
	// as a routing hint (global parts edited in a team context).
	teamTarget := ""
	if part.TeamID.Valid {
		teamTarget = part.TeamID.String()
	} else if v := r.URL.Query().Get("team_id"); v != "" {
		teamTarget = v
	}
	if teamTarget != "" {
		h.publishEvent(r, "part_changed", teamTarget, map[string]string{"part_id": part.ID.String()})
	}
	writeJSON(w, http.StatusOK, toPartFullResponse(part))
}

func (h *Handler) DeletePart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}
	if err := h.Queries.DeletePart(r.Context(), partID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete part", "INTERNAL")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

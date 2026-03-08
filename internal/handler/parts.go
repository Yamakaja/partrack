package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// numericString formats a pgtype.Numeric as a fixed-point decimal string.
// Returns nil when the value is not valid (NULL).
func numericString(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	if n.NaN {
		s := "NaN"
		return &s
	}
	isNeg := n.Int.Sign() < 0
	absInt := new(big.Int).Abs(n.Int)
	intStr := absInt.String()

	var result string
	if n.Exp >= 0 {
		result = intStr + strings.Repeat("0", int(n.Exp))
	} else {
		exp := int(-n.Exp)
		if len(intStr) <= exp {
			result = "0." + strings.Repeat("0", exp-len(intStr)) + intStr
		} else {
			result = intStr[:len(intStr)-exp] + "." + intStr[len(intStr)-exp:]
		}
	}
	if isNeg {
		result = "-" + result
	}
	return &result
}

// timestamptzString formats a pgtype.Timestamptz as an RFC3339 string.
// Returns nil when the value is not valid (NULL).
func timestamptzString(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
	return &s
}

// parsePaginationParams parses limit and offset from query parameters.
// limit defaults to 50, max 200. offset defaults to 0.
func parsePaginationParams(r *http.Request) (limit, offset int32) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return
}

type partFullResponse struct {
	ID                         string  `json:"id"`
	Scope                      string  `json:"scope"`
	TeamID                     *string `json:"team_id"`
	Mpn                        *string `json:"mpn"`
	Manufacturer               *string `json:"manufacturer"`
	Description                *string `json:"description"`
	ShortDescription           *string `json:"short_description"`
	Category                   *string `json:"category"`
	CategoryID                 *int32  `json:"category_id"`
	CategoryName               *string `json:"category_name"`
	CategorySource             string  `json:"category_source"`
	Package                    *string `json:"package"`
	DatasheetURL               *string `json:"datasheet_url"`
	ProductURL                 *string `json:"product_url"`
	ImageURL                   *string `json:"image_url"`
	LifecycleStatus            string  `json:"lifecycle_status"`
	EnrichmentSourceID         *string `json:"enrichment_source_id"`
	EnrichedAt                 *string `json:"enriched_at"`
	PreferredParameterSourceID *string `json:"preferred_parameter_source_id"`
	CreatedAt                  string  `json:"created_at"`
}

func toPartFullResponse(p dbgen.Part) partFullResponse {
	resp := partFullResponse{
		ID:               p.ID.String(),
		Scope:            string(p.Scope),
		Mpn:              p.Mpn,
		Manufacturer:     p.Manufacturer,
		Description:      p.Description,
		ShortDescription: p.ShortDescription,
		Category:         p.Category,
		CategoryID:       p.CategoryID,
		CategorySource:   p.CategorySource,
		Package:          p.Package,
		DatasheetURL:     p.DatasheetUrl,
		ProductURL:       p.ProductUrl,
		ImageURL:         p.ImageUrl,
		LifecycleStatus:  string(p.LifecycleStatus),
		CreatedAt:        p.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.TeamID.Valid {
		s := p.TeamID.String()
		resp.TeamID = &s
	}
	if p.EnrichmentSourceID.Valid {
		s := p.EnrichmentSourceID.String()
		resp.EnrichmentSourceID = &s
	}
	if p.EnrichedAt.Valid {
		s := p.EnrichedAt.Time.UTC().Format(time.RFC3339)
		resp.EnrichedAt = &s
	}
	if p.PreferredParameterSourceID.Valid {
		s := p.PreferredParameterSourceID.String()
		resp.PreferredParameterSourceID = &s
	}
	return resp
}

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

	// Parameter filters: param_{id}_min, param_{id}_max (numeric) and param_{id}_values (enum/text)
	type paramFilter struct {
		isNumeric bool
		min, max  float64
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
				f.min = v
			}
		case "max":
			if v, err := strconv.ParseFloat(vals[0], 64); err == nil {
				f.isNumeric = true
				f.max = v
			}
		case "values":
			f.values = vals // repeated query params: param_{id}_values=A&param_{id}_values=B
		}
	}

	if len(paramFilters) > 0 {
		teamID := params.TeamID
		// Collect matching part ID sets for each filter (AND semantics across filters)
		var idSets []map[string]struct{}
		for paramID, f := range paramFilters {
			var uuids []pgtype.UUID
			var queryErr error
			if f.isNumeric {
				pid := paramID
				uuids, queryErr = h.Queries.FindPartIDsByParameterRange(r.Context(), dbgen.FindPartIDsByParameterRangeParams{
					ParameterID:    &pid,
					NumericValue:   &f.min,
					NumericValue_2: &f.max,
					TeamID:         teamID,
				})
			} else if len(f.values) > 0 {
				pid := paramID
				uuids, queryErr = h.Queries.FindPartIDsByParameterValues(r.Context(), dbgen.FindPartIDsByParameterValuesParams{
					ParameterID: &pid,
					Column2:     f.values,
					TeamID:      teamID,
				})
			}
			if queryErr != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			set := make(map[string]struct{}, len(uuids))
			for _, u := range uuids {
				set[u.String()] = struct{}{}
			}
			idSets = append(idSets, set)
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
		ID                   string  `json:"id"`
		Scope                string  `json:"scope"`
		Mpn                  *string `json:"mpn"`
		Manufacturer         *string `json:"manufacturer"`
		Description          *string `json:"description"`
		ShortDescription     *string `json:"short_description"`
		Category             *string `json:"category"`
		CategoryID           *int32  `json:"category_id"`
		CategoryName         *string `json:"category_name"`
		CategorySource       string  `json:"category_source"`
		Package              *string `json:"package"`
		LifecycleStatus      string  `json:"lifecycle_status"`
		InventoryEntryID     *string `json:"inventory_entry_id"`
		QuantityOnHand       *int32  `json:"quantity_on_hand"`
		MinimumStockQuantity *int32  `json:"minimum_stock_quantity"`
		ReorderQuantity      *int32  `json:"reorder_quantity"`
		StorageLocationLabel *string `json:"storage_location_label"`
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
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if part.TeamID.Valid {
		h.publishEvent(r, "part_changed", part.TeamID.String(), map[string]string{"part_id": part.ID.String()})
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
	var (
		inventoryEntryID     *string
		quantityOnHand       *int32
		minimumStockQuantity *int32
		reorderQuantity      *int32
		preferredListingID   *string
		storageLocationID    *string
		storageLocationLabel *string
		categoryName         *string
		categoryParentID     *int32
	)

	teamIDStr := r.URL.Query().Get("team_id")
	var partBase dbgen.Part
	if teamIDStr != "" {
		var teamID pgtype.UUID
		if err := teamID.Scan(teamIDStr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
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
			s := row.StorageLocationID.String()
			storageLocationID = &s
		}
		storageLocationLabel = row.StorageLocationLabel
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

	type userParam struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit"`
	}
	type distParam struct {
		ID                    string  `json:"id"`
		Name                  string  `json:"name"`
		Value                 string  `json:"value"`
		Unit                  *string `json:"unit"`
		SourceDistributorID   string  `json:"source_distributor_id"`
		SourceDistributorName *string `json:"source_distributor_name"`
	}
	userParams := []userParam{}
	distParams := []distParam{}
	for _, p := range params {
		if p.SourceType == dbgen.ParameterSourceTypeUser {
			userParams = append(userParams, userParam{
				ID: p.ID.String(), Name: p.Name, Value: p.Value, Unit: p.Unit,
			})
		} else {
			distParams = append(distParams, distParam{
				ID:                    p.ID.String(),
				Name:                  p.Name,
				Value:                 p.Value,
				Unit:                  p.Unit,
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
		"storage_location_id":           storageLocationID,
		"storage_location_label":        storageLocationLabel,
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

// EnrichPart handles POST /api/v1/teams/{teamId}/parts/{partId}/enrich
// Triggers enrichment from a specific distributor SKU. Returns 202 with job_id.
// Body: {sku, distributor_id} (both required).
func (h *Handler) EnrichPart(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")

	var req struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SKU == "" || req.DistributorID == "" {
		writeError(w, http.StatusBadRequest, "sku and distributor_id are required", "BAD_REQUEST")
		return
	}

	if _, err := h.Queries.GetPart(r.Context(), partID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	jobID := h.enqueueEnrich(teamIDStr, partID.String(), req.SKU, req.DistributorID, user.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// ReEnrichAll handles POST /api/v1/teams/{teamId}/parts/enrich-all
// Enqueues enrichment for every part in the team's inventory that has a listing.
// Restricted to global admins only.
func (h *Handler) ReEnrichAll(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if !user.IsAdmin {
		writeError(w, http.StatusForbidden, "requires global admin", "FORBIDDEN")
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.GetPartsForReenrichment(r.Context(), teamID)
	if err != nil {
		slog.Error("GetPartsForReenrichment failed", "team_id", chi.URLParam(r, "teamId"), "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")
	for _, row := range rows {
		h.enqueueEnrich(teamIDStr, row.PartID.String(), row.Sku, row.DistributorID.String(), user.ID)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"job_count": len(rows)})
}

// enqueueEnrich enqueues a JobPartEnrich worker task and returns the job ID.
func (h *Handler) enqueueEnrich(teamID, partID, sku, distributorID, userID string) string {
	return h.Worker.Enqueue(worker.JobPartEnrich, teamID, map[string]string{
		"part_id":        partID,
		"sku":            sku,
		"distributor_id": distributorID,
		"user_id":        userID,
	})
}

// AddListing handles POST /api/v1/teams/{teamId}/parts/{partId}/listings
// Attaches a distributor listing to a part and triggers enrichment. Returns 202 with job_id.
// Body: {sku, distributor_id} (both required).
func (h *Handler) AddListing(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")

	var req struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SKU == "" || req.DistributorID == "" {
		writeError(w, http.StatusBadRequest, "sku and distributor_id are required", "BAD_REQUEST")
		return
	}

	if _, err := h.Queries.GetPart(r.Context(), partID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	user := middleware.UserFromContext(r.Context())
	jobID := h.enqueueEnrich(teamIDStr, partID.String(), req.SKU, req.DistributorID, user.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// ImportPart handles POST /api/v1/teams/{teamId}/parts/import
// Atomically imports a part from a distributor SKU: finds or creates a global-scoped part,
// upserts all listings and parameters, and ensures a team inventory entry exists.
// The distributor API call is made synchronously; no job is queued.
// Body: {sku, distributor_id} (both required). Returns 200 {part_id}.
func (h *Handler) ImportPart(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SKU == "" || req.DistributorID == "" {
		writeError(w, http.StatusBadRequest, "sku and distributor_id are required", "BAD_REQUEST")
		return
	}

	configs, err := h.Queries.ListEnabledDistributorConfigsForTeam(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	var cfg *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if configs[i].DistributorID.String() == req.DistributorID {
			cfg = &configs[i]
			break
		}
	}
	if cfg == nil {
		writeError(w, http.StatusBadRequest, "distributor not configured or not enabled for this team", "BAD_REQUEST")
		return
	}
	if cfg.ApiType == nil {
		writeError(w, http.StatusBadRequest, "distributor has no API integration", "BAD_REQUEST")
		return
	}
	client := h.Distributors.Lookup(*cfg.ApiType)
	if client == nil {
		writeError(w, http.StatusInternalServerError, "no client registered for this distributor", "INTERNAL_ERROR")
		return
	}

	result, err := client.EnrichPart(r.Context(), req.SKU, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		slog.Error("ImportPart: distributor API error", "sku", req.SKU, "err", err)
		writeError(w, http.StatusBadGateway, "distributor API error: "+err.Error(), "DISTRIBUTOR_ERROR")
		return
	}
	if result == nil {
		writeError(w, http.StatusNotFound, "part not found at distributor", "NOT_FOUND")
		return
	}

	partID, err := worker.ApplyEnrichmentResult(r.Context(), h.DB, h.Queries, teamID, cfg.DistributorID, result, nil, nil)
	if err != nil {
		slog.Error("ImportPart: apply enrichment failed", "sku", req.SKU, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to import part: "+err.Error(), "INTERNAL_ERROR")
		return
	}

	// Ensure the team has an inventory entry for this part.
	_, err = h.Queries.GetInventoryEntryByPartID(r.Context(), dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID,
		PartID: partID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = h.Queries.CreateInventoryEntry(r.Context(), dbgen.CreateInventoryEntryParams{
			TeamID: teamID,
			PartID: partID,
		})
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"part_id": partID.String()})
}

// DeleteListing handles DELETE /api/v1/teams/{teamId}/parts/{partId}/listings/{listingId}
// Removes a distributor listing and its distributor-sourced parameters. Returns 204.
func (h *Handler) DeleteListing(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}
	var listingID pgtype.UUID
	if err := listingID.Scan(chi.URLParam(r, "listingId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid listing ID", "BAD_REQUEST")
		return
	}

	teamIDStr := chi.URLParam(r, "teamId")

	listing, err := h.Queries.GetListingByPartAndID(r.Context(), dbgen.GetListingByPartAndIDParams{
		ID:     listingID,
		PartID: partID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "listing not found", "NOT_FOUND")
			return
		}
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
	if err := qtx.DeleteDistributorParameters(r.Context(), dbgen.DeleteDistributorParametersParams{
		PartID:              partID,
		SourceDistributorID: listing.DistributorID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := qtx.DeleteListing(r.Context(), listingID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "part_changed", teamIDStr, map[string]string{"part_id": partID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// ListParameters handles GET /api/v1/parts/{partId}/parameters
func (h *Handler) ListParameters(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	params, err := h.Queries.ListParameters(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

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
			userParams = append(userParams, userParam{base})
		} else {
			distParams = append(distParams, distParam{
				paramBase:             base,
				SourceDistributorID:   p.SourceDistributorID.String(),
				SourceDistributorName: p.SourceDistributorName,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_specified":      userParams,
		"distributor_sourced": distParams,
	})
}

// CreateParameter handles POST /api/v1/parts/{partId}/parameters
// Body: {name, value, unit?}
func (h *Handler) CreateParameter(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Name  string  `json:"name"`
		Value string  `json:"value"`
		Unit  *string `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "name and value are required", "BAD_REQUEST")
		return
	}

	param, err := h.Queries.UpsertUserParameter(r.Context(), dbgen.UpsertUserParameterParams{
		PartID: partID,
		Name:   req.Name,
		Value:  req.Value,
		Unit:   req.Unit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          param.ID.String(),
		"name":        param.Name,
		"value":       param.Value,
		"unit":        param.Unit,
		"source_type": string(param.SourceType),
	})
}

// DeleteParameter handles DELETE /api/v1/parts/{partId}/parameters/{parameterId}
func (h *Handler) DeleteParameter(w http.ResponseWriter, r *http.Request) {
	var paramID pgtype.UUID
	if err := paramID.Scan(chi.URLParam(r, "parameterId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid parameter ID", "BAD_REQUEST")
		return
	}

	if err := h.Queries.DeleteParameter(r.Context(), paramID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListListings handles GET /api/v1/parts/{partId}/listings
func (h *Handler) ListListings(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	listings, err := h.Queries.ListListings(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
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
	items := make([]item, len(listings))
	for i, l := range listings {
		var lastRef *string
		if l.LastRefreshedAt.Valid {
			s := l.LastRefreshedAt.Time.UTC().Format(time.RFC3339)
			lastRef = &s
		}
		items[i] = item{
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
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// RefreshListing handles POST /api/v1/teams/{teamId}/parts/{partId}/listings/{listingId}/refresh
// Triggers price/stock refresh for one listing. Returns 202 with job_id.
func (h *Handler) RefreshListing(w http.ResponseWriter, r *http.Request) {
	var listingID pgtype.UUID
	if err := listingID.Scan(chi.URLParam(r, "listingId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid listing ID", "BAD_REQUEST")
		return
	}

	listing, err := h.Queries.GetListing(r.Context(), listingID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "listing not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	teamID := chi.URLParam(r, "teamId")
	jobID := h.Worker.Enqueue(worker.JobListingRefresh, teamID, map[string]string{
		"listing_id":     listing.ID.String(),
		"part_id":        listing.PartID.String(),
		"distributor_id": listing.DistributorID.String(),
		"user_id":        userIDFromContext(r),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// ListPartCategories handles GET /api/v1/teams/{teamId}/parts/categories
// Returns the distinct set of category IDs present in the team's parts catalog.
func (h *Handler) ListPartCategories(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	rows, err := h.Queries.ListPartCategoryIDs(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	ids := make([]int32, 0, len(rows))
	for _, id := range rows {
		if id != nil {
			ids = append(ids, *id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"category_ids": ids})
}

// RefreshAllListings handles POST /api/v1/teams/{teamId}/parts/{partId}/listings/refresh
// Enqueues a stock/price refresh for every listing on the part. Returns 202 with job IDs.
func (h *Handler) RefreshAllListings(w http.ResponseWriter, r *http.Request) {
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	listings, err := h.Queries.ListListings(r.Context(), partID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	teamID := chi.URLParam(r, "teamId")
	userID := userIDFromContext(r)
	jobIDs := make([]string, 0, len(listings))
	for _, l := range listings {
		jobID := h.Worker.Enqueue(worker.JobListingRefresh, teamID, map[string]string{
			"listing_id":     l.ID.String(),
			"part_id":        l.PartID.String(),
			"distributor_id": l.DistributorID.String(),
			"user_id":        userID,
		})
		jobIDs = append(jobIDs, jobID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_ids": jobIDs})
}

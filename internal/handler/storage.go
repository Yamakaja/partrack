package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// gridLevel is a single dimension-level in a grid_config.
type gridLevel struct {
	Dims []int   `json:"dims"`
	Name *string `json:"name,omitempty"`
}

// gridConfig is the JSON shape of storage_locations.grid_config.
type gridConfig struct {
	Levels  []gridLevel `json:"levels"`
	Display *string     `json:"display,omitempty"` // "numeric" | "alphanumeric"
}

// validateGridConfig checks that a raw JSON grid_config is structurally valid.
func validateGridConfig(raw []byte) error {
	var cfg gridConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return errors.New("grid_config must be valid JSON")
	}
	if len(cfg.Levels) == 0 {
		return errors.New("grid_config.levels must have at least one entry")
	}
	for i, lv := range cfg.Levels {
		if len(lv.Dims) == 0 || len(lv.Dims) > 2 {
			return errors.New("each grid_config level must have 1 or 2 dims")
		}
		for _, d := range lv.Dims {
			if d <= 0 {
				_ = i
				return errors.New("grid_config dims must be positive integers")
			}
		}
	}
	if cfg.Display != nil && *cfg.Display != "numeric" && *cfg.Display != "alphanumeric" {
		return errors.New("grid_config.display must be \"numeric\" or \"alphanumeric\"")
	}
	return nil
}

// storageLocationItem is the JSON shape of a storage location in API responses.
type storageLocationItem struct {
	ID          string          `json:"id"`
	TeamID      string          `json:"team_id"`
	ParentID    *string         `json:"parent_id"`
	Label       string          `json:"label"`
	Description *string         `json:"description"`
	GridConfig  json.RawMessage `json:"grid_config"`
}

func storageLocationFromRow(id, teamID, parentID pgtype.UUID, label string, description *string, gridConfig []byte) storageLocationItem {
	item := storageLocationItem{
		ID:     id.String(),
		TeamID: teamID.String(),
		Label:  label,
	}
	if description != nil {
		item.Description = description
	}
	if parentID.Valid {
		s := parentID.String()
		item.ParentID = &s
	}
	if len(gridConfig) > 0 {
		item.GridConfig = json.RawMessage(gridConfig)
	}
	return item
}

// ListStorageLocations handles GET /api/v1/teams/{teamId}/storage-locations
func (h *Handler) ListStorageLocations(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	locations, err := h.Queries.ListStorageLocations(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items := make([]storageLocationItem, len(locations))
	for i, loc := range locations {
		items[i] = storageLocationFromRow(loc.ID, loc.TeamID, loc.ParentID, loc.Label, loc.Description, loc.GridConfig)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetStorageLocation handles GET /api/v1/teams/{teamId}/storage-locations/{locationId}
func (h *Handler) GetStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID, locationID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := locationID.Scan(chi.URLParam(r, "locationId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location ID", "BAD_REQUEST")
		return
	}

	loc, err := h.Queries.GetStorageLocation(r.Context(), dbgen.GetStorageLocationParams{
		ID:     locationID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "storage location not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	ancestors, err := h.Queries.ListStorageLocationAncestors(r.Context(), dbgen.ListStorageLocationAncestorsParams{
		LocationID: locationID,
		TeamID:     teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	ancestorItems := make([]storageLocationItem, len(ancestors))
	for i, a := range ancestors {
		ancestorItems[i] = storageLocationFromRow(a.ID, a.TeamID, a.ParentID, a.Label, a.Description, a.GridConfig)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"location":  storageLocationFromRow(loc.ID, loc.TeamID, loc.ParentID, loc.Label, loc.Description, loc.GridConfig),
		"ancestors": ancestorItems,
	})
}

// CreateStorageLocation handles POST /api/v1/teams/{teamId}/storage-locations
func (h *Handler) CreateStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Label       string          `json:"label"`
		Description *string         `json:"description"`
		ParentID    *string         `json:"parent_id"`
		GridConfig  json.RawMessage `json:"grid_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required", "BAD_REQUEST")
		return
	}

	var parentID pgtype.UUID
	if req.ParentID != nil {
		if err := parentID.Scan(*req.ParentID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid parent_id", "BAD_REQUEST")
			return
		}
		// Verify parent belongs to the same team.
		_, err := h.Queries.GetStorageLocation(r.Context(), dbgen.GetStorageLocationParams{
			ID:     parentID,
			TeamID: teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "parent location not found", "BAD_REQUEST")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	var rawGridConfig []byte
	if len(req.GridConfig) > 0 && string(req.GridConfig) != "null" {
		if err := validateGridConfig(req.GridConfig); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "BAD_REQUEST")
			return
		}
		rawGridConfig = []byte(req.GridConfig)
	}

	loc, err := h.Queries.CreateStorageLocation(r.Context(), dbgen.CreateStorageLocationParams{
		TeamID:      teamID,
		ParentID:    parentID,
		Label:       req.Label,
		Description: req.Description,
		GridConfig:  rawGridConfig,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a storage location with this label already exists in this parent", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "storage_changed", teamID.String(), map[string]string{})
	writeJSON(w, http.StatusCreated, storageLocationFromRow(loc.ID, loc.TeamID, loc.ParentID, loc.Label, loc.Description, loc.GridConfig))
}

// UpdateStorageLocation handles PATCH /api/v1/teams/{teamId}/storage-locations/{locationId}
func (h *Handler) UpdateStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID, locationID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := locationID.Scan(chi.URLParam(r, "locationId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Label       *string         `json:"label"`
		Description *string         `json:"description"`
		GridConfig  json.RawMessage `json:"grid_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	params := dbgen.UpdateStorageLocationParams{
		ID:     locationID,
		TeamID: teamID,
		Label:  req.Label,
	}

	// Description uses pointer-based COALESCE; GridConfig uses an explicit set flag.
	params.Description = req.Description

	if len(req.GridConfig) > 0 && string(req.GridConfig) != "null" {
		if err := validateGridConfig(req.GridConfig); err != nil {
			writeError(w, http.StatusBadRequest, err.Error(), "BAD_REQUEST")
			return
		}
		params.SetGridConfig = true
		params.GridConfig = []byte(req.GridConfig)
	} else if string(req.GridConfig) == "null" {
		// Explicit null clears the grid config.
		params.SetGridConfig = true
		params.GridConfig = nil
	}

	loc, err := h.Queries.UpdateStorageLocation(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "storage location not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "storage_changed", teamID.String(), map[string]string{})
	writeJSON(w, http.StatusOK, storageLocationFromRow(loc.ID, loc.TeamID, loc.ParentID, loc.Label, loc.Description, loc.GridConfig))
}

// DeleteStorageLocation handles DELETE /api/v1/teams/{teamId}/storage-locations/{locationId}
func (h *Handler) DeleteStorageLocation(w http.ResponseWriter, r *http.Request) {
	var teamID, locationID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := locationID.Scan(chi.URLParam(r, "locationId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location ID", "BAD_REQUEST")
		return
	}

	count, err := h.Queries.CountInventoryEntriesForLocation(r.Context(), locationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if count > 0 {
		writeError(w, http.StatusUnprocessableEntity, "storage location has inventory entries", "LOCATION_IN_USE")
		return
	}

	if err := h.Queries.DeleteStorageLocation(r.Context(), dbgen.DeleteStorageLocationParams{
		ID:     locationID,
		TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "storage_changed", teamID.String(), map[string]string{})
	w.WriteHeader(http.StatusNoContent)
}

// ListInventoryByLocation handles GET /api/v1/teams/{teamId}/storage-locations/{locationId}/inventory
func (h *Handler) ListInventoryByLocation(w http.ResponseWriter, r *http.Request) {
	var teamID, locationID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := locationID.Scan(chi.URLParam(r, "locationId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid location ID", "BAD_REQUEST")
		return
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountInventoryByLocation(r.Context(), dbgen.CountInventoryByLocationParams{
		LocationID: locationID,
		TeamID:     teamID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListInventoryByLocation(r.Context(), dbgen.ListInventoryByLocationParams{
		LocationID: locationID,
		TeamID:     teamID,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type item struct {
		ID                   string   `json:"id"`
		TeamID               string   `json:"team_id"`
		PartID               string   `json:"part_id"`
		PartMpn              *string  `json:"part_mpn"`
		PartDescription      *string  `json:"part_description"`
		QuantityOnHand       int32    `json:"quantity_on_hand"`
		StorageLocationID    *string  `json:"storage_location_id"`
		StorageCoordinate    []int32  `json:"storage_coordinate"`
		StorageLocationLabel *string  `json:"storage_location_label"`
		PreferredListingID   *string  `json:"preferred_listing_id"`
		MinimumStockQuantity *int32   `json:"minimum_stock_quantity"`
		ReorderQuantity      *int32   `json:"reorder_quantity"`
	}

	items := make([]item, len(rows))
	for i, row := range rows {
		it := item{
			ID:                   row.ID.String(),
			TeamID:               row.TeamID.String(),
			PartID:               row.PartID.String(),
			PartMpn:              row.PartMpn,
			PartDescription:      row.PartDescription,
			QuantityOnHand:       row.QuantityOnHand,
			StorageCoordinate:    row.StorageCoordinate,
			StorageLocationLabel: row.StorageLocationLabel,
			MinimumStockQuantity: row.MinimumStockQuantity,
			ReorderQuantity:      row.ReorderQuantity,
		}
		if row.StorageLocationID.Valid {
			s := row.StorageLocationID.String()
			it.StorageLocationID = &s
		}
		if row.PreferredListingID.Valid {
			s := row.PreferredListingID.String()
			it.PreferredListingID = &s
		}
		items[i] = it
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

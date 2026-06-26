package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/partmatch"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateBOMItem handles PATCH /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}
// Body: {resolved_part_id?} — manually resolve or re-resolve a BOM line item (pass null to clear).
//
//	{ignored?} — toggle ignore state on the item.
func (h *Handler) UpdateBOMItem(w http.ResponseWriter, r *http.Request) {
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
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	var body struct {
		ResolvedPartID *string `json:"resolved_part_id"`
		Ignored        *bool   `json:"ignored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	strPtr := func(s string) *string { return &s }

	// Ignore/un-ignore path.
	if body.Ignored != nil {
		row, err := h.Queries.IgnoreBOMLineItem(r.Context(), dbgen.IgnoreBOMLineItemParams{
			Ignored:       *body.Ignored,
			ID:            itemID,
			BomRevisionID: revID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                    row.ID.String(),
			"reference_designators": row.ReferenceDesignators,
			"quantity":              row.Quantity,
			"raw_description":       row.RawDescription,
			"resolved_part_id":      uuidPtrString(row.ResolvedPartID),
			"resolution_state":      row.ResolutionState,
			"ignored":               row.Ignored,
		})
		return
	}

	// Resolve/clear path.
	var resolvedPartID pgtype.UUID
	var resolutionState *string
	if body.ResolvedPartID != nil && *body.ResolvedPartID != "" {
		if err := resolvedPartID.Scan(*body.ResolvedPartID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid resolved_part_id", "BAD_REQUEST")
			return
		}
		resolutionState = strPtr("manual")
	}
	// If body.ResolvedPartID is nil or empty, resolvedPartID stays zero (NULL) and
	// resolutionState stays nil (NULL), which clears any prior resolution.

	row, err := h.Queries.UpdateBOMLineItem(r.Context(), dbgen.UpdateBOMLineItemParams{
		ResolvedPartID:  resolvedPartID,
		ResolutionState: resolutionState,
		ID:              itemID,
		BomRevisionID:   revID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                    row.ID.String(),
		"reference_designators": row.ReferenceDesignators,
		"quantity":              row.Quantity,
		"raw_description":       row.RawDescription,
		"resolved_part_id":      uuidPtrString(row.ResolvedPartID),
		"resolution_state":      row.ResolutionState,
		"ignored":               false,
	})
}

// AddSubstitute handles POST /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes
// Body: {part_id}
func (h *Handler) AddSubstitute(w http.ResponseWriter, r *http.Request) {
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
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	var body struct {
		PartID string `json:"part_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.PartID == "" {
		writeError(w, http.StatusBadRequest, "part_id is required", "BAD_REQUEST")
		return
	}

	var partID pgtype.UUID
	if err := partID.Scan(body.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.AddSubstitute(r.Context(), dbgen.AddSubstituteParams{
		BomLineItemID: itemID,
		PartID:        partID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "substitute already exists", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
	writeJSON(w, http.StatusCreated, map[string]any{
		"bom_line_item_id": itemID.String(),
		"part_id":          row.PartID.String(),
		"priority":         row.Priority,
	})
}

// ListSubstitutes handles GET /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/substitutes
// Returns all substitutes for all line items in the revision as a flat list.
func (h *Handler) ListSubstitutes(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	if !h.requireRevisionInTeam(w, r, revID, teamID) {
		return
	}

	rows, err := h.Queries.ListAllSubstitutesForRevision(r.Context(), revID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type subItem struct {
		BOMLineItemID string  `json:"bom_line_item_id"`
		PartID        string  `json:"part_id"`
		PartMPN       *string `json:"part_mpn"`
		Priority      int32   `json:"priority"`
	}
	items := make([]subItem, len(rows))
	for i, row := range rows {
		items[i] = subItem{
			BOMLineItemID: row.BomLineItemID.String(),
			PartID:        row.PartID.String(),
			PartMPN:       row.PartMpn,
			Priority:      row.Priority,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// RemoveSubstitute handles DELETE /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/substitutes/{partId}
func (h *Handler) RemoveSubstitute(w http.ResponseWriter, r *http.Request) {
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
	var revID pgtype.UUID
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part id", "BAD_REQUEST")
		return
	}
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	err := h.Queries.RemoveSubstitute(r.Context(), dbgen.RemoveSubstituteParams{
		BomLineItemID: itemID,
		PartID:        partID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
	w.WriteHeader(http.StatusNoContent)
}

// EnrichBOMItem handles POST /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/enrich
// Body: {sku, distributor_id} — fetches the SKU from the distributor API (find-or-create Part + Listings),
// then resolves the BOM line item to that Part.
func (h *Handler) EnrichBOMItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var teamID, projectID, revID, itemID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
	if err := revID.Scan(chi.URLParam(r, "revId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision id", "BAD_REQUEST")
		return
	}
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	var body struct {
		SKU           string `json:"sku"`
		DistributorID string `json:"distributor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.SKU == "" || body.DistributorID == "" {
		writeError(w, http.StatusBadRequest, "sku and distributor_id are required", "BAD_REQUEST")
		return
	}

	var distributorID pgtype.UUID
	if err := distributorID.Scan(body.DistributorID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid distributor_id", "BAD_REQUEST")
		return
	}

	// 1. Check for an existing local listing by (distributor_id, sku).
	var resolvedPartID pgtype.UUID
	var resolvedMPN, resolvedManufacturer string

	listing, err := h.Queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
		Sku:           body.SKU,
		DistributorID: distributorID,
	})
	if err == nil {
		resolvedPartID = listing.PartID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// 2. If not found locally, call the distributor API.
	if !resolvedPartID.Valid {
		cfg, err := h.Queries.GetDistributorConfigForSearch(ctx, dbgen.GetDistributorConfigForSearchParams{
			TeamID:        teamID,
			DistributorID: distributorID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusUnprocessableEntity, "distributor not configured or not enabled", "NOT_CONFIGURED")
			} else {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			}
			return
		}
		if cfg.ApiType == nil {
			writeError(w, http.StatusUnprocessableEntity, "distributor has no API integration", "NO_API")
			return
		}
		client := h.Distributors.Lookup(*cfg.ApiType)
		if client == nil {
			writeError(w, http.StatusUnprocessableEntity, "no client registered for distributor", "NO_CLIENT")
			return
		}

		result, err := client.EnrichPart(ctx, body.SKU, json.RawMessage(cfg.ApiCredentials))
		if err != nil {
			writeError(w, http.StatusBadGateway, "distributor API error: "+err.Error(), "API_ERROR")
			return
		}
		if result == nil {
			writeError(w, http.StatusNotFound, "SKU not found at distributor", "NOT_FOUND")
			return
		}

		partID, err := worker.ApplyEnrichmentResult(ctx, h.DB, h.Queries, teamID, distributorID, result, nil, nil, h.Manufacturers)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "distributor enrichment failed: "+err.Error(), "INTERNAL_ERROR")
			return
		}
		resolvedPartID = partID
		resolvedMPN = result.MPN
		resolvedManufacturer = result.Manufacturer
	}

	// 3. Resolve the BOM line item.
	manual := "manual"
	if _, err := h.Queries.UpdateBOMLineItem(ctx, dbgen.UpdateBOMLineItemParams{
		ResolvedPartID:  resolvedPartID,
		ResolutionState: &manual,
		ID:              itemID,
		BomRevisionID:   revID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return
	}

	// 4. Ensure inventory entry exists for (team, part).
	_, err = h.Queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID,
		PartID: resolvedPartID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = h.Queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
			TeamID: teamID,
			PartID: resolvedPartID,
		})
	}

	// If we enriched a new part, fetch its actual MPN/manufacturer from DB.
	if resolvedMPN == "" || resolvedManufacturer == "" {
		part, err := h.Queries.GetPart(ctx, resolvedPartID)
		if err == nil {
			if part.Mpn != nil {
				resolvedMPN = *part.Mpn
			}
			if part.Manufacturer != nil {
				resolvedManufacturer = *part.Manufacturer
			}
		}
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": revID.String()})
	writeJSON(w, http.StatusOK, map[string]any{
		"part_id":           resolvedPartID.String(),
		"part_mpn":          resolvedMPN,
		"part_manufacturer": resolvedManufacturer,
	})
}

// GenericPartCandidates handles
// GET /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}/items/{itemId}/generic-candidates
//
// For a line the parser read as a generic (parametric) spec, returns the existing
// catalog parts that satisfy it — category subtree + canonical package + primary
// value within the tolerance band. This is the same candidate set the import-time
// match-only tier used (which auto-resolves only on a unique match), so the user
// can resolve an ambiguous line by picking one. Empty for non-parametric lines or
// when nothing matches (then the user creates a generic part or searches).
func (h *Handler) GenericPartCandidates(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireLineItemInTeam(w, r, itemID, teamID) {
		return
	}

	raw, err := h.Queries.GetBOMLineItemInterpretation(r.Context(), itemID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	var interp genspec.Interpretation
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &interp) // UnmarshalJSON folds any legacy top-level package
	}

	rows, err := partmatch.Find(r.Context(), h.Queries, teamID, interp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type candidate struct {
		PartID         string   `json:"part_id"`
		Mpn            *string  `json:"mpn"`
		Manufacturer   *string  `json:"manufacturer"`
		Package        *string  `json:"package"`
		Description    *string  `json:"description"`
		ValueSI        *float64 `json:"value_si"`
		Scope          string   `json:"scope"`
		QuantityOnHand *int32   `json:"quantity_on_hand"`
	}
	out := make([]candidate, len(rows))
	for i, c := range rows {
		out[i] = candidate{
			PartID:         c.ID.String(),
			Mpn:            c.Mpn,
			Manufacturer:   c.Manufacturer,
			Package:        c.CanonicalPackage,
			Description:    c.Description,
			ValueSI:        c.ValueSi,
			Scope:          string(c.Scope),
			QuantityOnHand: c.QuantityOnHand,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

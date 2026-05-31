package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListRevisions handles GET /api/v1/teams/{teamId}/projects/{projectId}/revisions
func (h *Handler) ListRevisions(w http.ResponseWriter, r *http.Request) {
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}

	rows, err := h.Queries.ListRevisions(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type revItem struct {
		ID                    string  `json:"id"`
		Label                 string  `json:"label"`
		ImportedAt            string  `json:"imported_at"`
		Notes                 *string `json:"notes"`
		ImportedByDisplayName string  `json:"imported_by_display_name"`
		ItemCount             int64   `json:"item_count"`
		ResolvedCount         int64   `json:"resolved_count"`
		UnresolvedCount       int64   `json:"unresolved_count"`
		IsActive              bool    `json:"is_active"`
	}

	items := make([]revItem, len(rows))
	for i, r := range rows {
		items[i] = revItem{
			ID:                    r.ID.String(),
			Label:                 r.Label,
			ImportedAt:            r.ImportedAt.Time.UTC().Format(time.RFC3339),
			Notes:                 r.Notes,
			ImportedByDisplayName: r.ImportedByDisplayName,
			ItemCount:             r.ItemCount,
			ResolvedCount:         r.ResolvedCount,
			UnresolvedCount:       r.UnresolvedCount,
			IsActive:              r.IsActive,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ImportRevision handles POST /api/v1/teams/{teamId}/projects/{projectId}/revisions
// Body: {label, notes?, set_active, bom_import_config?, items: [
//
//	{
//	  reference_designators, quantity,
//	  raw_description?, manufacturer?, mpn?, name?, sku?, distributor_name?,
//	  resolved_distributor_id?
//	}
//
// ]}
func (h *Handler) ImportRevision(w http.ResponseWriter, r *http.Request) {
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

	// Decode body as raw map to detect optional bom_import_config.
	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var label string
	if v, ok := rawBody["label"]; ok {
		if err := json.Unmarshal(v, &label); err != nil || label == "" {
			writeError(w, http.StatusBadRequest, "label is required", "BAD_REQUEST")
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "label is required", "BAD_REQUEST")
		return
	}

	var notes *string
	if v, ok := rawBody["notes"]; ok {
		json.Unmarshal(v, &notes) //nolint:errcheck
	}

	var setActive bool
	if v, ok := rawBody["set_active"]; ok {
		json.Unmarshal(v, &setActive) //nolint:errcheck
	}

	type itemInput struct {
		ReferenceDesignators  string  `json:"reference_designators"`
		Quantity              int32   `json:"quantity"`
		RawDescription        *string `json:"raw_description"`
		Manufacturer          *string `json:"manufacturer"`
		MPN                   *string `json:"mpn"`
		Name                  *string `json:"name"`
		SKU                   *string `json:"sku"`
		DistributorName       *string `json:"distributor_name"`
		ResolvedDistributorID *string `json:"resolved_distributor_id"`
	}
	var items []itemInput
	if v, ok := rawBody["items"]; ok {
		if err := json.Unmarshal(v, &items); err != nil {
			writeError(w, http.StatusBadRequest, "invalid items", "BAD_REQUEST")
			return
		}
	}

	var bomImportConfigBytes []byte
	if v, ok := rawBody["bom_import_config"]; ok && string(v) != "null" {
		bomImportConfigBytes = []byte(v)
	}

	// All writes in a transaction.
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	rev, err := qtx.CreateRevision(r.Context(), dbgen.CreateRevisionParams{
		ProjectID:  projectID,
		Label:      label,
		Notes:      notes,
		ImportedBy: userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	for _, item := range items {
		var resolvedDistributorID pgtype.UUID
		if item.ResolvedDistributorID != nil && *item.ResolvedDistributorID != "" {
			if err := resolvedDistributorID.Scan(*item.ResolvedDistributorID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid resolved_distributor_id", "BAD_REQUEST")
				return
			}
		}

		_, err := qtx.CreateBOMLineItem(r.Context(), dbgen.CreateBOMLineItemParams{
			BomRevisionID:         rev.ID,
			ReferenceDesignators:  item.ReferenceDesignators,
			Quantity:              item.Quantity,
			RawDescription:        item.RawDescription,
			Manufacturer:          item.Manufacturer,
			Mpn:                   item.MPN,
			PartName:              item.Name,
			Sku:                   item.SKU,
			DistributorName:       item.DistributorName,
			ResolvedDistributorID: resolvedDistributorID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if setActive {
		if err := qtx.SetProjectActiveBOM(r.Context(), dbgen.SetProjectActiveBOMParams{
			ID:                  projectID,
			ActiveBomRevisionID: rev.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if bomImportConfigBytes != nil {
		if err := qtx.SetProjectBOMImportConfig(r.Context(), dbgen.SetProjectBOMImportConfigParams{
			ID:              projectID,
			BomImportConfig: bomImportConfigBytes,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": rev.ID.String()})

	jobID := h.Worker.Enqueue(worker.JobBOMImport, teamID.String(), map[string]string{
		"bom_revision_id": rev.ID.String(),
		"project_id":      projectID.String(),
		"user_id":         user.ID,
	})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":          jobID,
		"bom_revision_id": rev.ID.String(),
	})
}

// GetRevision handles GET /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}
func (h *Handler) GetRevision(w http.ResponseWriter, r *http.Request) {
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

	rev, err := h.Queries.GetRevision(r.Context(), dbgen.GetRevisionParams{
		ID:        revID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "revision not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	lineItems, err := h.Queries.ListBOMLineItemsWithResolveData(r.Context(), revID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	subs, err := h.Queries.ListAllSubstitutesForRevision(r.Context(), revID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Index substitutes by BOM line item ID.
	type subEntry struct {
		PartID  string  `json:"part_id"`
		PartMpn *string `json:"part_mpn"`
	}
	subsByItem := make(map[string][]subEntry)
	for _, s := range subs {
		key := s.BomLineItemID.String()
		subsByItem[key] = append(subsByItem[key], subEntry{
			PartID:  s.PartID.String(),
			PartMpn: s.PartMpn,
		})
	}

	type resolveItemResp struct {
		ID             string          `json:"id"`
		Status         *string         `json:"status"`
		TaskType       *string         `json:"task_type"`
		EnrichmentData json.RawMessage `json:"enrichment_data,omitempty"`
		ErrorMessage   *string         `json:"error_message,omitempty"`
	}

	type lineItemResp struct {
		ID                   string           `json:"id"`
		ReferenceDesignators string           `json:"reference_designators"`
		Quantity             int32            `json:"quantity"`
		PartName             *string          `json:"part_name"`
		RawDescription       *string          `json:"raw_description"`
		Manufacturer         *string          `json:"manufacturer"`
		MPN                  *string          `json:"mpn"`
		SKU                  *string          `json:"sku"`
		DistributorName      string           `json:"distributor_name"`
		HasLinkedDistributor bool             `json:"has_linked_distributor"`
		ResolvedPartID       *string          `json:"resolved_part_id"`
		ResolvedPartMpn      *string          `json:"resolved_part_mpn"`
		ResolutionState      *string          `json:"resolution_state"`
		Ignored              bool             `json:"ignored"`
		Substitutes          []subEntry       `json:"substitutes"`
		ResolveItem          *resolveItemResp `json:"resolve_item,omitempty"`
	}

	itemsResp := make([]lineItemResp, len(lineItems))
	for i, li := range lineItems {
		id := li.ID.String()
		itemsResp[i] = lineItemResp{
			ID:                   id,
			ReferenceDesignators: li.ReferenceDesignators,
			Quantity:             li.Quantity,
			PartName:             li.PartName,
			RawDescription:       li.RawDescription,
			Manufacturer:         li.Manufacturer,
			MPN:                  li.Mpn,
			SKU:                  li.Sku,
			DistributorName:      li.DistributorName,
			HasLinkedDistributor: li.ResolvedDistributorID.Valid,
			ResolvedPartID:       uuidPtrString(li.ResolvedPartID),
			ResolvedPartMpn:      li.ResolvedPartMpn,
			ResolutionState:      li.ResolutionState,
			Ignored:              li.Ignored,
			Substitutes:          subsByItem[id],
		}
		if li.ResolveItemID.Valid {
			ri := &resolveItemResp{
				ID:           li.ResolveItemID.String(),
				Status:       li.ResolveStatus,
				TaskType:     li.ResolveTaskType,
				ErrorMessage: li.ResolveErrorMessage,
			}
			if len(li.ResolveEnrichmentData) > 0 {
				ri.EnrichmentData = json.RawMessage(li.ResolveEnrichmentData)
			}
			itemsResp[i].ResolveItem = ri
		}
		if itemsResp[i].Substitutes == nil {
			itemsResp[i].Substitutes = []subEntry{}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                       rev.ID.String(),
		"label":                    rev.Label,
		"imported_at":              rev.ImportedAt.Time.UTC().Format(time.RFC3339),
		"notes":                    rev.Notes,
		"imported_by_display_name": rev.ImportedByDisplayName,
		"is_active":                rev.IsActive,
		"items":                    itemsResp,
	})
}

// UpdateRevision handles PATCH /api/v1/teams/{teamId}/projects/{projectId}/revisions/{revId}
// Body: {label?, notes?}
func (h *Handler) UpdateRevision(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Label *string `json:"label"`
		Notes *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.UpdateRevision(r.Context(), dbgen.UpdateRevisionParams{
		Label:     body.Label,
		Notes:     body.Notes,
		ID:        revID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "revision not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "revision_changed", teamID.String(), map[string]string{"project_id": projectID.String(), "revision_id": row.ID.String()})
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          row.ID.String(),
		"label":       row.Label,
		"imported_at": row.ImportedAt.Time.UTC().Format(time.RFC3339),
		"notes":       row.Notes,
	})
}

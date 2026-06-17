package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListRevisions handles GET /api/v1/teams/{teamId}/projects/{projectId}/revisions
func (h *Handler) ListRevisions(w http.ResponseWriter, r *http.Request) {
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
	if !h.requireProjectInTeam(w, r, projectID, teamID) {
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
	if !h.requireProjectInTeam(w, r, projectID, teamID) {
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
		ReferenceDesignators  string          `json:"reference_designators"`
		Quantity              int32           `json:"quantity"`
		RawDescription        *string         `json:"raw_description"`
		Manufacturer          *string         `json:"manufacturer"`
		MPN                   *string         `json:"mpn"`
		Name                  *string         `json:"name"`
		SKU                   *string         `json:"sku"`
		DistributorName       *string         `json:"distributor_name"`
		ResolvedDistributorID *string         `json:"resolved_distributor_id"`
		Value                 *string         `json:"value"`
		Footprint             *string         `json:"footprint"`
		Interpretation        json.RawMessage `json:"interpretation"`
		InterpretationSource  *string         `json:"interpretation_source"`
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

	// Interpretation reuse (doc/bom-import.md §9.2): seed each line's
	// interpretation from prior knowledge so recurring passives don't demand
	// re-review every revision. priorInterp carries this project's previous
	// revision forward; teamMemory replays remembered user corrections across
	// projects. See resolveReusedInterpretation for the precedence.
	priorInterp, teamMemory, err := buildInterpretationReuse(r.Context(), qtx, teamID, projectID, rev.ID)
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

		// Determine the line's interpretation. Precedence:
		//   1. Client override from the review gate (an explicit user correction)
		//      — persisted as 'user' and remembered team-wide for future imports.
		//   2. A prior-revision *user* edit for the same key (most specific intent).
		//   3. A remembered team correction for the same key.
		//   4. A prior-revision *auto* interpretation (avoids re-flagging a part
		//      the user already reviewed last revision).
		//   5. A fresh parse.
		// A 'user' source is never clobbered by a re-parse.
		interpBytes := []byte(item.Interpretation)
		interpSource := "auto"
		switch {
		case len(interpBytes) > 0:
			// Review-gate override: a user edit or an LLM-derived interpretation.
			// Both are confirmed readings — persisted as-is and remembered team-wide
			// so the same value+footprint is reused (and never re-sent to the LLM).
			interpSource = "user"
			if item.InterpretationSource != nil && (*item.InterpretationSource == "user" || *item.InterpretationSource == "llm") {
				interpSource = *item.InterpretationSource
			}
			if interpSource == "user" || interpSource == "llm" {
				if err := h.rememberInterpretation(r.Context(), qtx, teamID, userID, item.Value, item.Footprint, interpBytes); err != nil {
					writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
					return
				}
			}
		default:
			if rb, rs, ok := resolveReusedInterpretation(priorInterp, teamMemory, item.ReferenceDesignators, derefStr(item.Value), derefStr(item.Footprint)); ok {
				interpBytes, interpSource = rb, rs
			} else {
				interp := genspec.Interpret(genspec.Input{
					Refdes:    item.ReferenceDesignators,
					Footprint: derefStr(item.Footprint),
					Value:     derefStr(item.Value),
					MPN:       derefStr(item.MPN),
					SKU:       derefStr(item.SKU),
				})
				interpBytes, _ = json.Marshal(interp)
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
			Value:                 item.Value,
			Footprint:             item.Footprint,
			Interpretation:        interpBytes,
			InterpretationSource:  interpSource,
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
	// The team-feed milestone (bom.imported, with resolution stats) is emitted by
	// the import job on completion — see worker.handleBOMImport.

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
	if !h.requireRevisionInTeam(w, r, revID, teamID) {
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

	lineItems, err := h.Queries.ListBOMLineItemsWithResolveData(r.Context(), dbgen.ListBOMLineItemsWithResolveDataParams{
		TeamID:        teamID,
		BomRevisionID: revID,
	})
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
		ResolvedPartFlag     *string          `json:"resolved_part_flag,omitempty"`
		ResolutionState      *string          `json:"resolution_state"`
		Ignored              bool             `json:"ignored"`
		Value                *string          `json:"value,omitempty"`
		Footprint            *string          `json:"footprint,omitempty"`
		Interpretation       json.RawMessage  `json:"interpretation,omitempty"`
		InterpretationSource string           `json:"interpretation_source,omitempty"`
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
			Value:                li.Value,
			Footprint:            li.Footprint,
			Interpretation:       li.Interpretation,
			InterpretationSource: li.InterpretationSource,
			Substitutes:          subsByItem[id],
		}
		if li.ResolvedPartFlag.Valid && li.ResolvedPartFlag.PartFlag != dbgen.PartFlagNone {
			f := string(li.ResolvedPartFlag.PartFlag)
			itemsResp[i].ResolvedPartFlag = &f
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
	if !h.requireRevisionInTeam(w, r, revID, teamID) {
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

// rememberedInterp is a carried-over interpretation plus its source, used while
// seeding a new revision's line interpretations from the previous one.
type rememberedInterp struct {
	bytes  []byte
	source string
}

// interpKeySep separates the components of an interpretation-memory key. ASCII
// unit separator — cannot appear in a class key, value or footprint.
const interpKeySep = "\x1f"

// classKey builds the interpretation-memory lookup key from an already-resolved
// class and normalised value/footprint keys. Returns "" when the line cannot be
// keyed (no class, or neither a value nor a footprint), signalling "do not
// reuse" — a bare class is too coarse to carry a remembered interpretation.
func classKey(class, valueKey, footprintKey string) string {
	if class == "" || (valueKey == "" && footprintKey == "") {
		return ""
	}
	return class + interpKeySep + valueKey + interpKeySep + footprintKey
}

// lineInterpKey builds the reuse key for an incoming BOM line from its raw
// designators, value and footprint, deriving the class from the designator.
func lineInterpKey(refdes, value, footprint string) string {
	class, _ := genspec.ClassifyRefdes(refdes)
	return classKey(class, genspec.NormalizeKey(value), genspec.NormalizeKey(footprint))
}

// buildInterpretationReuse loads the two reuse sources for a team: priorInterp
// (the previous revision of projectID, when projectID is valid) and teamMemory
// (the team's remembered corrections, always loaded). Both are keyed for lookup
// with classKey/lineInterpKey. excludeRevID is the in-progress revision to skip
// when finding the prior one (pass the freshly-created revision id at import).
func buildInterpretationReuse(ctx context.Context, q *dbgen.Queries, teamID, projectID, excludeRevID pgtype.UUID) (map[string]rememberedInterp, map[string][]byte, error) {
	prior := map[string]rememberedInterp{}
	if projectID.Valid {
		if priorRevID, err := q.GetImmediatelyPriorRevision(ctx, dbgen.GetImmediatelyPriorRevisionParams{
			ProjectID:         projectID,
			CurrentRevisionID: excludeRevID,
		}); err == nil {
			items, err := q.ListBOMLineItemInterpretations(ctx, priorRevID)
			if err != nil {
				return nil, nil, err
			}
			for _, pi := range items {
				if len(pi.Interpretation) == 0 {
					continue
				}
				if key := lineInterpKey(pi.ReferenceDesignators, derefStr(pi.Value), derefStr(pi.Footprint)); key != "" {
					prior[key] = rememberedInterp{bytes: pi.Interpretation, source: pi.InterpretationSource}
				}
			}
		}
	}

	mem := map[string][]byte{}
	rows, err := q.ListInterpretationMemory(ctx, teamID)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range rows {
		if key := classKey(m.Class, m.ValueKey, m.FootprintKey); key != "" {
			mem[key] = m.Interpretation
		}
	}
	return prior, mem, nil
}

// resolveReusedInterpretation picks a remembered interpretation for a line, or
// reports ok=false to fall back to a fresh parse. Precedence:
//  1. a prior-revision *user* edit (most specific deliberate decision),
//  2. a remembered team correction,
//  3. a prior-revision *auto* interpretation (so a reviewed line does not
//     re-flag). A 'user' result must not be clobbered by a re-parse.
//
// Both maps may be nil (e.g. preview supplies only teamMemory).
func resolveReusedInterpretation(prior map[string]rememberedInterp, teamMemory map[string][]byte, refdes, value, footprint string) (bytes []byte, source string, ok bool) {
	key := lineInterpKey(refdes, value, footprint)
	if key == "" {
		return nil, "", false
	}
	if ri := prior[key]; ri.source == "user" {
		return ri.bytes, "user", true
	}
	if b := teamMemory[key]; b != nil {
		return b, "user", true
	}
	if ri := prior[key]; ri.bytes != nil {
		return ri.bytes, ri.source, true
	}
	return nil, "", false
}

// rememberInterpretation records a user-confirmed interpretation in the team's
// interpretation memory so the same correction is reused on future imports. It
// is a no-op for interpretations that cannot be keyed (no class, or neither a
// value nor a footprint). Runs inside the import transaction.
func (h *Handler) rememberInterpretation(ctx context.Context, qtx *dbgen.Queries, teamID, userID pgtype.UUID, value, footprint *string, interpBytes []byte) error {
	var it genspec.Interpretation
	if err := json.Unmarshal(interpBytes, &it); err != nil || it.Class == "" {
		return nil //nolint:nilerr // unkeyable interpretation: nothing to remember
	}
	valueKey := genspec.NormalizeKey(derefStr(value))
	footprintKey := genspec.NormalizeKey(derefStr(footprint))
	if valueKey == "" && footprintKey == "" {
		return nil
	}
	return qtx.UpsertInterpretationMemory(ctx, dbgen.UpsertInterpretationMemoryParams{
		TeamID:         teamID,
		Class:          it.Class,
		ValueKey:       valueKey,
		FootprintKey:   footprintKey,
		Interpretation: interpBytes,
		CreatedBy:      userID,
	})
}

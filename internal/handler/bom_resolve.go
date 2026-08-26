package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Yamakaja/partrack/internal/apitypes"
	"github.com/Yamakaja/partrack/internal/bomresolve"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxResolvePreviewItems bounds a single resolve preview.
//
// It is far lower than the interpretation preview's limit because the work is
// not comparable: interpretation is a string parse, while each line here walks a
// ladder of catalog queries. A few hundred lines is a large BOM; anything past
// this is a client doing something other than checking one.
const maxResolvePreviewItems = 1000

// ResolvePreview handles POST /api/v1/teams/{teamId}/bom/resolve-preview.
//
// It answers "what would importing this BOM actually do?" — per line, which
// resolution tier fires, which part it lands on, and what the alternatives were
// when it is ambiguous. Nothing is written: no revision, no line items, no
// parts, no inventory entries.
//
// The answer comes from bomresolve.Resolve, the same ladder the import job
// walks. That sharing is the entire point. A preview that reimplemented the
// tiers would agree with the importer only until one of them changed, and a
// forecast that is confidently wrong is worse than no forecast — it is exactly
// the failure this endpoint exists to prevent.
//
// Two honest limits remain, and they are properties of previewing rather than
// bugs. A line whose state is "importing" would be handed to the distributor
// resolve pipeline, whose outcome depends on a live API call this endpoint does
// not make. And the ladder is evaluated against the catalog as it stands now:
// an import that creates parts along the way (an earlier line resolving through
// a distributor) can change what a later line matches.
func (h *Handler) ResolvePreview(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var req apitypes.BOMResolvePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required", "BAD_REQUEST")
		return
	}
	if len(req.Items) > maxResolvePreviewItems {
		writeError(w, http.StatusBadRequest, "too many items", "TOO_MANY_ITEMS")
		return
	}

	// A project unlocks the prior-revision tier, and its remembered corrections
	// are what the import would read. requireProjectInTeam closes the
	// cross-tenant gap: membership alone does not entitle you to a project id.
	var projectID pgtype.UUID
	projectMemory := map[string][]byte{}
	if req.ProjectID != "" {
		if err := projectID.Scan(req.ProjectID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id", "BAD_REQUEST")
			return
		}
		if !h.requireProjectInTeam(w, r, projectID, teamID) {
			return
		}
		// As in the interpretation preview, only project memory is consulted:
		// every prior-revision user edit is also in memory, and a prior auto
		// reading equals a fresh parse (doc/bom-import.md §9.2).
		if _, mem, err := buildInterpretationReuse(r.Context(), h.Queries, projectID, pgtype.UUID{}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		} else {
			projectMemory = mem
		}
	}

	scope := bomresolve.Scope{TeamID: teamID, ProjectID: projectID}

	out := apitypes.BOMResolvePreview{
		Items:   make([]apitypes.BOMResolvePreviewItem, 0, len(req.Items)),
		Summary: map[string]int{},
	}

	for i, item := range req.Items {
		interp, source := previewInterpretation(item, projectMemory)

		decision := bomresolve.Resolve(r.Context(), h.Queries, scope, bomresolve.Line{
			ReferenceDesignators:  item.ReferenceDesignators,
			Mpn:                   item.MPN,
			Manufacturer:          item.Manufacturer,
			Sku:                   item.SKU,
			Value:                 item.Value,
			Footprint:             item.Footprint,
			ResolvedDistributorID: scanUUID(item.ResolvedDistributorID),
			Interpretation:        interp,
		})

		entry := apitypes.BOMResolvePreviewItem{
			Index:                i,
			ReferenceDesignators: item.ReferenceDesignators,
			State:                decision.State,
			Tier:                 decision.Tier,
			Route:                string(interp.Route),
			Category:             interp.Category,
			InterpretationSource: source,
		}
		if decision.Resolved() {
			entry.Part = h.candidateForPart(r.Context(), decision.PartID, decision)
		}
		if decision.State == bomresolve.StateAmbiguous {
			entry.Candidates = ambiguityCandidates(decision)
		}

		out.Items = append(out.Items, entry)
		out.Summary[decision.State]++
	}

	writeJSON(w, http.StatusOK, out)
}

// previewInterpretation picks the reading the import would use for a line: the
// client's own (the review gate's correction), else the project's remembered
// one, else a fresh parse.
func previewInterpretation(item apitypes.BOMImportItem, projectMemory map[string][]byte) (genspec.Interpretation, string) {
	var interp genspec.Interpretation

	if len(item.Interpretation) > 0 {
		if err := json.Unmarshal(item.Interpretation, &interp); err == nil {
			return interp, "supplied"
		}
	}
	if b, _, ok := resolveReusedInterpretation(nil, projectMemory,
		item.ReferenceDesignators, derefStr(item.Value), derefStr(item.Footprint)); ok {
		if err := json.Unmarshal(b, &interp); err == nil {
			return interp, "reused"
		}
	}
	return genspec.Interpret(genspec.Input{
		Refdes:    item.ReferenceDesignators,
		Footprint: derefStr(item.Footprint),
		Value:     derefStr(item.Value),
		MPN:       derefStr(item.MPN),
		SKU:       derefStr(item.SKU),
	}), "auto"
}

// candidateForPart describes the part a line resolved to. Most tiers already
// carry the details in Candidates, so only the SKU and prior-revision tiers —
// which return a bare id — cost a lookup.
func (h *Handler) candidateForPart(ctx context.Context, partID pgtype.UUID, d bomresolve.Decision) *apitypes.BOMResolveCandidate {
	for _, c := range d.Candidates {
		if c.ID == partID {
			cand := toWireCandidate(c)
			return &cand
		}
	}
	part, err := h.Queries.GetPart(ctx, partID)
	if err != nil {
		// A tier matched it a moment ago; report the link without the detail
		// rather than failing the whole preview over a display field.
		return &apitypes.BOMResolveCandidate{PartID: partID.String()}
	}
	return &apitypes.BOMResolveCandidate{
		PartID:       partID.String(),
		Mpn:          part.Mpn,
		Manufacturer: part.Manufacturer,
		Package:      part.Package,
		Description:  part.Description,
		Scope:        string(part.Scope),
	}
}

// ambiguityCandidates renders the set a human would be asked to choose from.
func ambiguityCandidates(d bomresolve.Decision) []apitypes.BOMResolveCandidate {
	if len(d.Candidates) == 0 {
		return nil
	}
	out := make([]apitypes.BOMResolveCandidate, len(d.Candidates))
	for i, c := range d.Candidates {
		out[i] = toWireCandidate(c)
	}
	return out
}

func toWireCandidate(c bomresolve.Candidate) apitypes.BOMResolveCandidate {
	return apitypes.BOMResolveCandidate{
		PartID:         c.ID.String(),
		Mpn:            c.Mpn,
		Manufacturer:   c.Manufacturer,
		Package:        c.Package,
		Description:    c.Description,
		Scope:          c.Scope,
		QuantityOnHand: c.QuantityOnHand,
	}
}

// scanUUID parses an optional uuid string, yielding the invalid zero value when
// absent or malformed — which the tiers read as "no distributor", the same as
// the import would.
func scanUUID(s *string) pgtype.UUID {
	var id pgtype.UUID
	if s != nil && *s != "" {
		_ = id.Scan(*s) //nolint:errcheck // an unparseable id is simply absent
	}
	return id
}

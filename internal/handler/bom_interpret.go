package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
)

// maxInterpretPreviewItems bounds a single preview request.
const maxInterpretPreviewItems = 5000

// previewItem is one interpretation plus its provenance. source is "auto" (fresh
// parse), "reused" (team interpretation memory), or "llm" (LLM fallback). The
// Interpretation fields are flattened into the same JSON object.
type previewItem struct {
	genspec.Interpretation
	Source string `json:"source"`
	Reused bool   `json:"reused"` // retained for the existing memory badge; == (source == "reused")
}

// InterpretPreview handles POST /api/v1/bom/interpret-preview.
//
// It parses mapped BOM rows into structured interpretations so the import UI can
// show how each row was read before committing — nothing is persisted. When a
// team_id is supplied (and the caller is a member), the team's remembered
// corrections are layered on top, so the review gate shows the SAME
// interpretation the import will persist instead of a fresh parse the import
// would then silently override. Each item carries a `reused` flag for the UI.
func (h *Handler) InterpretPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID string `json:"team_id"`
		Items  []struct {
			Refdes    string            `json:"refdes"`
			Footprint string            `json:"footprint"`
			Value     string            `json:"value"`
			Mpn       string            `json:"mpn"`
			Sku       string            `json:"sku"`
			Row       map[string]string `json:"row"` // original header → cell, for the LLM
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if len(req.Items) > maxInterpretPreviewItems {
		writeError(w, http.StatusBadRequest, "too many items", "BAD_REQUEST")
		return
	}

	// Optionally layer the team's remembered corrections over the fresh parse.
	// Only team memory is consulted (no project scope): every prior-revision
	// *user* edit is also in team memory, and a prior *auto* interpretation is
	// identical to the fresh parse, so team memory reproduces every visible reuse.
	var teamMemory map[string][]byte
	var teamID pgtype.UUID
	if req.TeamID != "" {
		if err := teamID.Scan(req.TeamID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
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
		if _, err := h.Queries.CheckTeamMembership(r.Context(), dbgen.CheckTeamMembershipParams{
			TeamID: teamID,
			UserID: userID,
		}); err != nil {
			writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
			return
		}
		// projectID invalid → prior-revision carryover skipped; teamMemory loaded.
		if _, mem, err := buildInterpretationReuse(r.Context(), h.Queries, teamID, pgtype.UUID{}, pgtype.UUID{}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		} else {
			teamMemory = mem
		}
	}

	// Phase 1: memory reuse, else a fresh deterministic parse.
	out := make([]previewItem, len(req.Items))
	for i, it := range req.Items {
		if rb, _, ok := resolveReusedInterpretation(nil, teamMemory, it.Refdes, it.Value, it.Footprint); ok {
			var remembered genspec.Interpretation
			if err := json.Unmarshal(rb, &remembered); err == nil {
				out[i] = previewItem{Interpretation: remembered, Source: "reused", Reused: true}
				continue
			}
		}
		out[i] = previewItem{
			Interpretation: genspec.Interpret(genspec.Input{
				Refdes: it.Refdes, Footprint: it.Footprint, Value: it.Value, MPN: it.Mpn, SKU: it.Sku,
			}),
			Source: "auto",
		}
	}

	// Phase 2: LLM fallback for rows the parser couldn't read cleanly OR couldn't
	// resolve at all. Runs only when configured + team-scoped; bounded in count and
	// concurrency. Each goroutine writes a distinct index. curated categories and
	// distributors are loaded once and shared.
	if h.LLM != nil && teamID.Valid {
		var escalate []int
		maxEsc := h.Config.LLM.MaxEscalationsPerPreview
		for i := range out {
			if llmEligible(out[i], req.Items[i].Mpn, req.Items[i].Sku) {
				if maxEsc > 0 && len(escalate) >= maxEsc {
					break
				}
				escalate = append(escalate, i)
			}
		}
		curated, _ := h.curatedCategories(r.Context())
		if len(escalate) > 0 && len(curated) > 0 {
			dists, _ := h.Queries.ListDistributors(r.Context())
			g, gctx := errgroup.WithContext(r.Context())
			g.SetLimit(6)
			for _, idx := range escalate {
				idx := idx
				it := req.Items[idx]
				row := it.Row
				if len(row) == 0 {
					row = map[string]string{"Reference": it.Refdes, "Footprint": it.Footprint, "Value": it.Value, "MPN": it.Mpn, "SKU": it.Sku}
				}
				g.Go(func() error {
					if interp, ok := h.escalateInterpretation(gctx, teamID, row, out[idx].Interpretation, curated, dists); ok {
						out[idx] = previewItem{Interpretation: interp, Source: "llm"}
					}
					return nil
				})
			}
			_ = g.Wait()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// llmEligible reports whether a previewed row should be sent to the LLM fallback:
// a fresh ('auto') row with no mapped MPN/SKU column that the parser flagged
// (escalate), couldn't classify (unknown), or read as a bare identifier in the
// value field (identity) — i.e. it won't otherwise resolve cleanly. Clean generic
// rows, and rows already carrying an identity column, are left alone.
func llmEligible(p previewItem, mappedMPN, mappedSKU string) bool {
	if p.Source != "auto" {
		return false
	}
	if strings.TrimSpace(mappedMPN) != "" || strings.TrimSpace(mappedSKU) != "" {
		return false
	}
	return p.Escalate || p.Route == genspec.RouteUnknown || p.Route == genspec.RouteIdentity
}

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"
)

// maxInterpretPreviewItems bounds a single preview request.
const maxInterpretPreviewItems = 5000

// interpretReqItem is one mapped BOM row to interpret. Row is the original
// header→cell map, given to the LLM so it sees what the parser discards.
type interpretReqItem struct {
	Refdes    string            `json:"refdes"`
	Footprint string            `json:"footprint"`
	Value     string            `json:"value"`
	Mpn       string            `json:"mpn"`
	Sku       string            `json:"sku"`
	Row       map[string]string `json:"row"`
}

// previewItem is one interpretation plus its provenance. source is "auto" (fresh
// parse), "reused" (team interpretation memory), or "llm" (LLM fallback). The
// Interpretation fields are flattened into the same JSON object. AI is the ephemeral
// record of an LLM read (review-only; never persisted).
type previewItem struct {
	genspec.Interpretation
	Source string        `json:"source"`
	Reused bool          `json:"reused"` // retained for the existing memory badge; == (source == "reused")
	AI     *aiProvenance `json:"ai,omitempty"`
}

// InterpretPreview handles POST /api/v1/bom/interpret-preview.
//
// It parses mapped BOM rows into structured interpretations so the import UI can
// show how each row was read before committing — nothing is persisted. When a
// team_id is supplied (and the caller is a member), the team's remembered
// corrections are layered on top, so the review gate shows the SAME interpretation
// the import will persist instead of a fresh parse the import would then silently
// override. Each item carries a `reused` flag for the UI.
//
// Phase 1 (deterministic parse + memory reuse) is always synchronous. Phase 2 (the
// LLM fallback) is slow, so when the caller sets `stream:true` it is run
// asynchronously: the response returns the Phase-1 items immediately along with a
// `job_id` and the `pending` indices being escalated, and each LLM result streams
// back over SSE (`bom_interpret_item`, then `bom_interpret_complete`) scoped to the
// requesting user. Without `stream`, Phase 2 runs inline (used by single-row
// re-parse after an edit).
func (h *Handler) InterpretPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID string             `json:"team_id"`
		Stream bool               `json:"stream"`
		Items  []interpretReqItem `json:"items"`
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
	out := buildPhase1(req.Items, teamMemory)

	// Eligible rows for the LLM fallback (only when configured + team-scoped).
	var eligible []int
	if h.LLM != nil && teamID.Valid {
		maxEsc := h.Config.LLM.MaxEscalationsPerPreview
		for i := range out {
			if llmEligible(out[i], req.Items[i].Mpn, req.Items[i].Sku) {
				if maxEsc > 0 && len(eligible) >= maxEsc {
					break
				}
				eligible = append(eligible, i)
			}
		}
	}

	// Phase 2 (async): stream LLM results over SSE, returning Phase-1 immediately.
	if req.Stream && len(eligible) > 0 {
		jobID := uuid.NewString()
		userID := userIDFromContext(r)
		// Detach from the request context (which ends when this response is written).
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		go func() {
			defer cancel()
			h.streamEscalations(ctx, jobID, req.TeamID, userID, teamID, req.Items, out, eligible)
		}()
		writeJSON(w, http.StatusOK, map[string]any{"items": out, "job_id": jobID, "pending": eligible})
		return
	}

	// Phase 2 (sync): run the LLM fallback inline. Each goroutine writes a distinct
	// index. curated categories and distributors are loaded once and shared.
	if len(eligible) > 0 {
		curated, _ := h.curatedCategories(r.Context())
		if len(curated) > 0 {
			dists, _ := h.Queries.ListDistributors(r.Context())
			g, gctx := errgroup.WithContext(r.Context())
			g.SetLimit(6)
			for _, idx := range eligible {
				idx := idx
				it := req.Items[idx]
				row := escalateRow(it)
				g.Go(func() error {
					if interp, prov, ok := h.escalateInterpretation(gctx, teamID, row, out[idx].Interpretation, curated, dists); ok {
						p := prov
						out[idx] = previewItem{Interpretation: interp, Source: "llm", AI: &p}
					}
					return nil
				})
			}
			_ = g.Wait()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// buildPhase1 produces the deterministic / memory-reuse reading for every row.
func buildPhase1(items []interpretReqItem, teamMemory map[string][]byte) []previewItem {
	out := make([]previewItem, len(items))
	for i, it := range items {
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
	return out
}

// escalateRow is the raw row handed to the LLM, falling back to the mapped fields
// when no original row was supplied.
func escalateRow(it interpretReqItem) map[string]string {
	if len(it.Row) > 0 {
		return it.Row
	}
	return map[string]string{"Reference": it.Refdes, "Footprint": it.Footprint, "Value": it.Value, "MPN": it.Mpn, "SKU": it.Sku}
}

// streamEscalations runs the LLM fallback for the eligible rows and publishes each
// result (or error) over SSE to the requesting user, then a terminal completion
// event. Runs in its own goroutine on a detached context.
func (h *Handler) streamEscalations(ctx context.Context, jobID, teamIDStr, userID string, teamID pgtype.UUID, items []interpretReqItem, base []previewItem, eligible []int) {
	curated, _ := h.curatedCategories(ctx)
	if len(curated) == 0 {
		h.publishInterpretEvent(teamIDStr, userID, jobID, "bom_interpret_complete", map[string]any{"total": 0, "errors": 0})
		return
	}
	dists, _ := h.Queries.ListDistributors(ctx)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6)
	var errCount atomic.Int64
	for _, idx := range eligible {
		idx := idx
		row := escalateRow(items[idx])
		g.Go(func() error {
			interp, prov, ok := h.escalateInterpretation(gctx, teamID, row, base[idx].Interpretation, curated, dists)
			ev := map[string]any{"index": idx}
			if ok {
				p := prov
				ev["status"] = "ok"
				ev["item"] = previewItem{Interpretation: interp, Source: "llm", AI: &p}
			} else {
				errCount.Add(1)
				ev["status"] = "error"
				ev["error"] = "interpretation unavailable"
			}
			h.publishInterpretEvent(teamIDStr, userID, jobID, "bom_interpret_item", ev)
			return nil
		})
	}
	_ = g.Wait()
	h.publishInterpretEvent(teamIDStr, userID, jobID, "bom_interpret_complete", map[string]any{
		"total": len(eligible), "errors": int(errCount.Load()),
	})
}

// publishInterpretEvent sends an interpret SSE event only to the requesting user.
func (h *Handler) publishInterpretEvent(teamID, userID, jobID, eventType string, data map[string]any) {
	b, _ := json.Marshal(data)
	h.Worker.Publish(worker.Event{Type: eventType, Data: b, JobID: jobID, TeamID: teamID, TargetUserID: userID})
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

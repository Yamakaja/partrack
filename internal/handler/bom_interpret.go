package handler

import (
	"encoding/json"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxInterpretPreviewItems bounds a single preview request.
const maxInterpretPreviewItems = 5000

// previewItem is one interpretation plus whether it was supplied from the team's
// interpretation memory rather than parsed fresh. The Interpretation fields are
// flattened into the same JSON object.
type previewItem struct {
	genspec.Interpretation
	Reused bool `json:"reused"`
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
			Refdes    string `json:"refdes"`
			Footprint string `json:"footprint"`
			Value     string `json:"value"`
			Mpn       string `json:"mpn"`
			Sku       string `json:"sku"`
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
	if req.TeamID != "" {
		var teamID pgtype.UUID
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

	out := make([]previewItem, len(req.Items))
	for i, it := range req.Items {
		if rb, _, ok := resolveReusedInterpretation(nil, teamMemory, it.Refdes, it.Value, it.Footprint); ok {
			var remembered genspec.Interpretation
			if err := json.Unmarshal(rb, &remembered); err == nil {
				out[i] = previewItem{Interpretation: remembered, Reused: true}
				continue
			}
		}
		out[i] = previewItem{Interpretation: genspec.Interpret(genspec.Input{
			Refdes:    it.Refdes,
			Footprint: it.Footprint,
			Value:     it.Value,
			MPN:       it.Mpn,
			SKU:       it.Sku,
		})}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

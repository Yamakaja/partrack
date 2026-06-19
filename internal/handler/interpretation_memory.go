package handler

import (
	"encoding/json"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// interpMemoryEntry is one remembered interpretation as surfaced to the project
// management UI. The raw interpretation JSON is passed through so the client can
// render the same human-readable summary the import review gate shows.
type interpMemoryEntry struct {
	ID             string          `json:"id"`
	Class          string          `json:"class"`
	ValueKey       string          `json:"value_key"`
	FootprintKey   string          `json:"footprint_key"`
	Interpretation json.RawMessage `json:"interpretation"`
	CreatedBy      *string         `json:"created_by"`
	CreatedByName  *string         `json:"created_by_name"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

// ListInterpretationMemory handles
// GET /api/v1/teams/{teamId}/projects/{projectId}/interpretation-memory.
//
// Returns the project's remembered BOM-line interpretations (doc/bom-import.md
// §9.2) so a user can inspect — and, via DeleteInterpretationMemoryEntry, prune —
// the corrections that future imports of this project will reuse. Viewer+.
func (h *Handler) ListInterpretationMemory(w http.ResponseWriter, r *http.Request) {
	var teamID, projectID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
	if !h.requireProjectInTeam(w, r, projectID, teamID) {
		return
	}

	rows, err := h.Queries.ListInterpretationMemoryEntries(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	entries := make([]interpMemoryEntry, len(rows))
	for i, m := range rows {
		entries[i] = interpMemoryEntry{
			ID:             m.ID.String(),
			Class:          m.Class,
			ValueKey:       m.ValueKey,
			FootprintKey:   m.FootprintKey,
			Interpretation: json.RawMessage(m.Interpretation),
			CreatedBy:      uuidPtrString(m.CreatedBy),
			CreatedByName:  m.CreatedByName,
			CreatedAt:      m.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      m.UpdatedAt.Time.Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

// DeleteInterpretationMemoryEntry handles
// DELETE /api/v1/teams/{teamId}/projects/{projectId}/interpretation-memory/{memoryId}.
//
// Removes one remembered interpretation so future imports re-parse that line
// fresh instead of reusing a stale or wrong correction. The project_id filter in
// the query closes cross-tenant access at the data layer; a non-matching id
// (already gone, or another project's) is a 404. Member+.
func (h *Handler) DeleteInterpretationMemoryEntry(w http.ResponseWriter, r *http.Request) {
	var teamID, projectID, memoryID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}
	if err := memoryID.Scan(chi.URLParam(r, "memoryId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid memory id", "BAD_REQUEST")
		return
	}
	if !h.requireProjectInTeam(w, r, projectID, teamID) {
		return
	}

	n, err := h.Queries.DeleteInterpretationMemoryEntry(r.Context(), dbgen.DeleteInterpretationMemoryEntryParams{
		ID:        memoryID,
		ProjectID: projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "interpretation memory entry not found", "NOT_FOUND")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

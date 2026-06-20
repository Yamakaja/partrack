package handler

import (
	"net/http"

	"github.com/Yamakaja/partrack/internal/changelog"
)

// Changelog returns the curated user-facing changelog. With `?since=<version>`
// it returns only releases newer than that version (used by the frontend to
// show a "what's changed" dialog after an update); without it, all releases.
// Public, like /version — the entries are release notes, not sensitive data.
func (h *Handler) Changelog(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	writeJSON(w, http.StatusOK, map[string]any{"releases": changelog.Since(since)})
}

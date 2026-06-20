package handler

import (
	"net/http"

	"github.com/Yamakaja/partrack/internal/version"
)

// Version reports the running build's version metadata. Like Health/Ready it is
// public (no auth): it is useful to ops tooling, and the frontend fetches it to
// render the build in the sidebar footer (and, later, to drive the "what's
// changed" dialog) before the user is necessarily known.
func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, version.Get())
}

package handler

import (
	"net/http"

	"github.com/Yamakaja/partrack/internal/timing"
)

// AdminMetrics returns a snapshot of the in-memory request/query timing
// registry: slowest queries, slowest routes, and the heaviest users/projects by
// database time. Aggregates are ephemeral (rotate on a fixed window) and reset
// when the process restarts. Admin-only.
func (h *Handler) AdminMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, timing.Default().Snapshot())
}

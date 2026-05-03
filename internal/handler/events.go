package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
)

// StreamEvents handles GET /api/v1/teams/{teamId}/events
// Opens a Server-Sent Events stream for the team. Events pushed:
//   - bom_import_progress
//   - part_enrichment_progress
//   - listing_refresh_done
//   - order_status_update
func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request) {
	// Verify the client supports SSE.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported", "STREAMING_UNSUPPORTED")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	// Send an initial comment to confirm the connection.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	teamID := chi.URLParam(r, "teamId")
	user := middleware.UserFromContext(r.Context())
	events, cancel := h.Worker.Subscribe(teamID, user.ID)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			wrapper := map[string]any{
				"type":   ev.Type,
				"job_id": ev.JobID,
				"data":   json.RawMessage(ev.Data),
			}
			wrapperJSON, _ := json.Marshal(wrapper)
			fmt.Fprintf(w, "data: %s\n\n", wrapperJSON)
			flusher.Flush()
		case <-ticker.C:
			pingJSON, _ := json.Marshal(map[string]any{"type": "ping"})
			fmt.Fprintf(w, "data: %s\n\n", pingJSON)
			flusher.Flush()
		}
	}
}

package handler

import (
	"fmt"
	"net/http"

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
	events, cancel := h.Worker.Subscribe(teamID)
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-events:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data)
			flusher.Flush()
		}
	}
}

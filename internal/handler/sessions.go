package handler

import (
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListSessions handles GET /api/v1/sessions
// Lists all active (non-expired) sessions for the current user.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
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

	sessions, err := h.Queries.ListSessionsByUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type sessionItem struct {
		ID          string  `json:"id"`
		CreatedAt   string  `json:"created_at"`
		ExpiresAt   string  `json:"expires_at"`
		LastSeenAt  string  `json:"last_seen_at"`
		UserAgent   *string `json:"user_agent"`
		IPAddress   *string `json:"ip_address"`
		IsCurrent   bool    `json:"is_current"`
	}

	currentSessionID := user.SessionID()
	items := make([]sessionItem, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{
			ID:         s.ID.String(),
			CreatedAt:  s.CreatedAt.Time.UTC().Format(time.RFC3339),
			ExpiresAt:  s.ExpiresAt.Time.UTC().Format(time.RFC3339),
			LastSeenAt: s.LastSeenAt.Time.UTC().Format(time.RFC3339),
			UserAgent:  s.UserAgent,
			IPAddress:  s.IpAddress,
			IsCurrent:  s.ID.String() == currentSessionID,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":  len(items),
		"limit":  len(items),
		"offset": 0,
		"items":  items,
	})
}

// RevokeSession handles DELETE /api/v1/sessions/{sessionId}
// Revokes a session belonging to the current user.
func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}

	var sessionID, userID pgtype.UUID
	if err := sessionID.Scan(chi.URLParam(r, "sessionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID", "BAD_REQUEST")
		return
	}
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	_, err := h.Queries.DeleteSessionByID(r.Context(), dbgen.DeleteSessionByIDParams{
		ID:     sessionID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.Cache.EvictSessionByID(sessionID.String())
	w.WriteHeader(http.StatusNoContent)
}

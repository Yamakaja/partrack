package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetPreferences handles GET /api/v1/preferences
// Returns the current user's preferences blob; returns {} if none saved yet.
func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
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

	prefs, err := h.Queries.GetPreferences(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"preferences": map[string]any{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// prefs is []byte (raw JSON from the DB); embed directly.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"preferences":`))   //nolint:errcheck
	w.Write(prefs)                        //nolint:errcheck
	w.Write([]byte(`}`))                  //nolint:errcheck
}

// PutPreferences handles PUT /api/v1/preferences
// Replaces the entire preferences blob. Body must be a valid JSON object.
func (h *Handler) PutPreferences(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB max
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body", "BAD_REQUEST")
		return
	}

	// Validate: must be a JSON object (not array, string, number, null…)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		writeError(w, http.StatusBadRequest, "preferences must be a JSON object", "BAD_REQUEST")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	saved, err := h.Queries.UpsertPreferences(r.Context(), dbgen.UpsertPreferencesParams{
		UserID:      userID,
		Preferences: body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"preferences":`)) //nolint:errcheck
	w.Write(saved)                      //nolint:errcheck
	w.Write([]byte(`}`))               //nolint:errcheck
}

package handler

import (
	"encoding/json"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// tokenPrefix is prepended to raw API tokens so they're recognisable in logs
// and secrets scanners.
const tokenPrefix = "ptk_"

// ListTokens handles GET /api/v1/tokens
// Lists the current user's API tokens; token hashes are never returned.
func (h *Handler) ListTokens(w http.ResponseWriter, r *http.Request) {
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

	tokens, err := h.Queries.ListAPITokensByUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type tokenItem struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		CreatedAt  string  `json:"created_at"`
		LastUsedAt *string `json:"last_used_at"`
		ExpiresAt  *string `json:"expires_at"`
	}

	items := make([]tokenItem, len(tokens))
	for i, t := range tokens {
		item := tokenItem{
			ID:        t.ID.String(),
			Name:      t.Name,
			CreatedAt: t.CreatedAt.Time.UTC().Format(time.RFC3339),
		}
		if t.LastUsedAt.Valid {
			s := t.LastUsedAt.Time.UTC().Format(time.RFC3339)
			item.LastUsedAt = &s
		}
		if t.ExpiresAt.Valid {
			s := t.ExpiresAt.Time.UTC().Format(time.RFC3339)
			item.ExpiresAt = &s
		}
		items[i] = item
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":  len(items),
		"limit":  len(items),
		"offset": 0,
		"items":  items,
	})
}

// CreateToken handles POST /api/v1/tokens
// Creates an API token; the raw token is returned exactly once.
func (h *Handler) CreateToken(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}

	var req struct {
		Name      string  `json:"name"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "BAD_REQUEST")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rawSuffix, err := generateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	rawToken := tokenPrefix + rawSuffix
	tokenHash := hashToken(rawToken)

	var expiresAt pgtype.Timestamptz
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339", "BAD_REQUEST")
			return
		}
		expiresAt = pgtype.Timestamptz{Time: t, Valid: true}
	}

	token, err := h.Queries.CreateAPIToken(r.Context(), dbgen.CreateAPITokenParams{
		UserID:    userID,
		Name:      req.Name,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	resp := map[string]any{
		"id":         token.ID.String(),
		"name":       token.Name,
		"token":      rawToken, // returned once
		"created_at": token.CreatedAt.Time.UTC().Format(time.RFC3339),
		"expires_at": nil,
	}
	if token.ExpiresAt.Valid {
		resp["expires_at"] = token.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}

	w.Header().Set("Location", "/api/v1/tokens/"+token.ID.String())
	writeJSON(w, http.StatusCreated, resp)
}

// RevokeToken handles DELETE /api/v1/tokens/{tokenId}
// Revokes an API token belonging to the current user.
func (h *Handler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}

	var tokenID, userID pgtype.UUID
	if err := tokenID.Scan(chi.URLParam(r, "tokenId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid token ID", "BAD_REQUEST")
		return
	}
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := h.Queries.DeleteAPITokenByID(r.Context(), dbgen.DeleteAPITokenByIDParams{
		ID:     tokenID,
		UserID: userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.Cache.EvictAPITokenByID(tokenID.String())
	w.WriteHeader(http.StatusNoContent)
}

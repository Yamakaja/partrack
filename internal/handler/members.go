package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// validRoles is the set of accepted role strings.
var validRoles = map[string]bool{
	"viewer": true, "member": true, "purchaser": true, "admin": true,
}

// ListMembers handles GET /api/v1/teams/{teamId}/members
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	members, err := h.Queries.ListTeamMembers(r.Context(), teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type memberItem struct {
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Role        string `json:"role"`
		JoinedAt    string `json:"joined_at"`
	}
	items := make([]memberItem, len(members))
	for i, m := range members {
		items[i] = memberItem{
			UserID:      m.UserID.String(),
			DisplayName: m.DisplayName,
			Email:       m.Email,
			Role:        string(m.Role),
			JoinedAt:    m.JoinedAt.Time.UTC().Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": len(items), "limit": len(items), "offset": 0, "items": items,
	})
}

// AddMember handles POST /api/v1/teams/{teamId}/members
// Body: {user_id, role}
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" || req.Role == "" {
		writeError(w, http.StatusBadRequest, "user_id and role are required", "BAD_REQUEST")
		return
	}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "role must be one of: viewer, member, purchaser, admin", "BAD_REQUEST")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id", "BAD_REQUEST")
		return
	}

	// Verify the user exists.
	if _, err := h.Queries.GetUserByID(r.Context(), userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	membership, err := h.Queries.CreateTeamMembership(r.Context(), dbgen.CreateTeamMembershipParams{
		TeamID: teamID,
		UserID: userID,
		Role:   dbgen.TeamRole(req.Role),
	})
	if err != nil {
		// pgx wraps unique-constraint violations; check the error message.
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "user is already a member of this team", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	w.Header().Set("Location", "/api/v1/teams/"+teamID.String()+"/members/"+userID.String())
	writeJSON(w, http.StatusCreated, map[string]any{
		"team_id":    membership.TeamID.String(),
		"user_id":    membership.UserID.String(),
		"role":       string(membership.Role),
		"created_at": membership.CreatedAt.Time.UTC().Format(time.RFC3339),
	})
}

// UpdateMember handles PATCH /api/v1/teams/{teamId}/members/{userId}
// Body: {role}
func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	var teamID, userID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" {
		writeError(w, http.StatusBadRequest, "role is required", "BAD_REQUEST")
		return
	}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "role must be one of: viewer, member, purchaser, admin", "BAD_REQUEST")
		return
	}

	membership, err := h.Queries.UpdateTeamMemberRole(r.Context(), dbgen.UpdateTeamMemberRoleParams{
		TeamID: teamID,
		UserID: userID,
		Role:   dbgen.TeamRole(req.Role),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "member not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"team_id":    membership.TeamID.String(),
		"user_id":    membership.UserID.String(),
		"role":       string(membership.Role),
		"created_at": membership.CreatedAt.Time.UTC().Format(time.RFC3339),
	})
}

// RemoveMember handles DELETE /api/v1/teams/{teamId}/members/{userId}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	var teamID, userID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID", "BAD_REQUEST")
		return
	}

	if err := h.Queries.RemoveTeamMember(r.Context(), dbgen.RemoveTeamMemberParams{
		TeamID: teamID,
		UserID: userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeactivateUser handles POST /api/v1/teams/{teamId}/members/{userId}/deactivate
// Globally deactivates the user (is_active = false). They cannot log in anywhere.
func (h *Handler) DeactivateUser(w http.ResponseWriter, r *http.Request) {
	var userID pgtype.UUID
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user ID", "BAD_REQUEST")
		return
	}

	if err := h.Queries.DeactivateUser(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// isUniqueViolation returns true if err is a PostgreSQL unique constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type pgError interface{ SQLState() string }
	var pg pgError
	return errors.As(err, &pg) && pg.SQLState() == "23505"
}

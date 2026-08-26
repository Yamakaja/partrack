package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/apitypes"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type teamResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
}

func toTeamResponse(t dbgen.Team) teamResponse {
	return teamResponse{
		ID:          t.ID.String(),
		Name:        t.Name,
		Description: t.Description,
		CreatedAt:   t.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListTeams handles GET /api/v1/teams
// Lists teams the current user is a member of.
func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
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

	var items []apitypes.Team
	if user.IsAdmin {
		teams, err := h.Queries.ListAllTeams(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		items = make([]apitypes.Team, len(teams))
		for i, t := range teams {
			items[i] = apitypes.Team{
				ID:          t.ID.String(),
				Name:        t.Name,
				Description: t.Description,
				CreatedAt:   t.CreatedAt.Time.UTC().Format(time.RFC3339),
				Role:        "admin",
			}
		}
	} else {
		teams, err := h.Queries.ListUserTeams(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		items = make([]apitypes.Team, len(teams))
		for i, t := range teams {
			items[i] = apitypes.Team{
				ID:          t.ID.String(),
				Name:        t.Name,
				Description: t.Description,
				CreatedAt:   t.CreatedAt.Time.UTC().Format(time.RFC3339),
				Role:        string(t.Role),
			}
		}
	}

	writeJSON(w, http.StatusOK, apitypes.List[apitypes.Team]{
		Total: int64(len(items)), Limit: int32(len(items)), Offset: 0, Items: items,
	})
}

// CreateTeam handles POST /api/v1/teams
// Creates a new team; the calling user is automatically added as admin.
func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
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

	// Create team and membership in a single transaction.
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := h.Queries.WithTx(tx)

	team, err := qtx.CreateTeam(r.Context(), dbgen.CreateTeamParams{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	_, err = qtx.CreateTeamMembership(r.Context(), dbgen.CreateTeamMembershipParams{
		TeamID: team.ID,
		UserID: userID,
		Role:   dbgen.TeamRoleAdmin,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	w.Header().Set("Location", "/api/v1/teams/"+team.ID.String())
	writeJSON(w, http.StatusCreated, toTeamResponse(team))
}

// GetTeam handles GET /api/v1/teams/{teamId}
// Returns team details.
func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	team, err := h.Queries.GetTeam(r.Context(), teamID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "team not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, toTeamResponse(team))
}

// UpdateTeam handles PATCH /api/v1/teams/{teamId}
// Updates team name or description.
func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	team, err := h.Queries.UpdateTeam(r.Context(), dbgen.UpdateTeamParams{
		ID:          teamID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "team not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, toTeamResponse(team))
}

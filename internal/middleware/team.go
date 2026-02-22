package middleware

import (
	"context"
	"errors"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// roleOrder maps role names to a numeric level for comparison.
// Higher = more privileged.
var roleOrder = map[string]int{
	"viewer":    1,
	"member":    2,
	"purchaser": 3,
	"admin":     4,
}

// RequireTeamRole returns middleware that:
//  1. Reads {teamId} from the URL.
//  2. Loads the current user's membership in that team from the database.
//  3. Rejects with 403 if the membership role is below minRole.
//  4. Injects the TeamMembership into the context for downstream handlers.
//
// Must be used after Authenticate in the middleware chain.
func (m *Middleware) RequireTeamRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				writeUnauthorized(w, "authentication required")
				return
			}

			teamIDStr := chi.URLParam(r, "teamId")
			if teamIDStr == "" {
				writeForbidden(w, "team ID required")
				return
			}

			var teamID pgtype.UUID
			if err := teamID.Scan(teamIDStr); err != nil {
				writeForbidden(w, "invalid team ID")
				return
			}

			var userID pgtype.UUID
			if err := userID.Scan(user.ID); err != nil {
				writeForbidden(w, "invalid user ID")
				return
			}

			membership, err := m.lookupMembership(r.Context(), teamID, userID)
			if err != nil || membership == nil {
				writeForbidden(w, "not a member of this team")
				return
			}

			if roleOrder[membership.Role] < roleOrder[minRole] {
				writeForbidden(w, "insufficient role")
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyTeamMembership, membership)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// lookupMembership fetches the team membership for (teamID, userID).
func (m *Middleware) lookupMembership(ctx context.Context, teamID, userID pgtype.UUID) (*TeamMembership, error) {
	row, err := m.q.GetTeamMembership(ctx, dbgen.GetTeamMembershipParams{
		TeamID: teamID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}
		return nil, err
	}
	return &TeamMembership{
		TeamID: row.TeamID.String(),
		UserID: row.UserID.String(),
		Role:   string(row.Role),
	}, nil
}

func writeForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"` + msg + `","code":"FORBIDDEN"}`)) //nolint:errcheck
}

package handler

import (
	"context"
	"errors"
	"net/http"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Object-level authorization guards for the project/revision/BOM subtree.
//
// RequireTeamRole only proves the caller belongs to the {teamId} in the URL; it
// does not check that nested path/body IDs belong to that team. These helpers
// anchor the chain to the team, fail-closed: a foreign or non-existent id yields
// 404 and false, and the caller must return immediately. The owning team is
// resolved through the immutable resource-owner cache (DB only on a miss), so the
// common case is a map lookup, not a query.

// ownerLoader resolves the owning team for a resource id on a cache miss.
type ownerLoader func(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)

// resolveOwnerTeam returns the owning teamID string for a resource, consulting
// the resource-owner cache before falling back to load. found is false when the
// resource does not exist.
func (h *Handler) resolveOwnerTeam(ctx context.Context, resourceID pgtype.UUID, load ownerLoader) (team string, found bool, err error) {
	id := resourceID.String()
	if cached, ok := h.Cache.LookupResourceOwner(id); ok {
		return cached, true, nil
	}
	teamID, err := load(ctx, resourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	h.Cache.StoreResourceOwner(id, teamID.String())
	return teamID.String(), true, nil
}

// guardOwner resolves the owning team for resourceID and verifies it equals
// teamID. On mismatch / not-found it writes 404, on DB error 500; returns false
// in both cases. notFoundMsg names the resource for the 404 body.
func (h *Handler) guardOwner(w http.ResponseWriter, r *http.Request, resourceID, teamID pgtype.UUID, load ownerLoader, notFoundMsg string) bool {
	owner, found, err := h.resolveOwnerTeam(r.Context(), resourceID, load)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return false
	}
	if !found || owner != teamID.String() {
		writeError(w, http.StatusNotFound, notFoundMsg, "NOT_FOUND")
		return false
	}
	return true
}

// requireProjectInTeam verifies projectID belongs to teamID.
func (h *Handler) requireProjectInTeam(w http.ResponseWriter, r *http.Request, projectID, teamID pgtype.UUID) bool {
	return h.guardOwner(w, r, projectID, teamID, h.Queries.ProjectTeam, "project not found")
}

// requireRevisionInTeam verifies revID belongs to teamID (revision→project→team).
func (h *Handler) requireRevisionInTeam(w http.ResponseWriter, r *http.Request, revID, teamID pgtype.UUID) bool {
	return h.guardOwner(w, r, revID, teamID, h.Queries.RevisionTeam, "revision not found")
}

// requireLineItemInTeam verifies itemID belongs to teamID (item→revision→project→team).
func (h *Handler) requireLineItemInTeam(w http.ResponseWriter, r *http.Request, itemID, teamID pgtype.UUID) bool {
	return h.guardOwner(w, r, itemID, teamID, h.Queries.BOMLineItemTeam, "item not found")
}

// requireTeamMembership verifies the authenticated user is a member of teamID.
// Used by global (non-/teams) routes that accept a ?team_id= parameter and would
// otherwise expose team-scoped data without a RequireTeamRole middleware. Site
// admins pass implicitly. Writes 401/403/500 and returns false on failure.
func (h *Handler) requireTeamMembership(w http.ResponseWriter, r *http.Request, teamID pgtype.UUID) bool {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return false
	}
	if user.IsAdmin {
		return true
	}
	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return false
	}
	m, err := h.checkTeamMembership(r.Context(), teamID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return false
	}
	if m == nil {
		writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
		return false
	}
	return true
}

// requirePartManualEdit authorizes a MANUAL edit (user-specified parameter) to a
// part. User parameters are subjective overrides ("user-specified values always
// win"), so editing them on a shared global part requires instance admin; on a
// team-scoped part it requires membership in the owning team. Returns the part
// and true, else writes 403/404/500 and returns false.
func (h *Handler) requirePartManualEdit(w http.ResponseWriter, r *http.Request, partID pgtype.UUID) (dbgen.Part, bool) {
	part, err := h.Queries.GetPart(r.Context(), partID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return dbgen.Part{}, false
	}
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return dbgen.Part{}, false
	}
	if user.IsAdmin {
		return part, true
	}
	if string(part.Scope) == "global" {
		writeError(w, http.StatusForbidden, "global parts can only be edited by an administrator", "FORBIDDEN")
		return dbgen.Part{}, false
	}
	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return dbgen.Part{}, false
	}
	m, err := h.checkTeamMembership(r.Context(), part.TeamID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return dbgen.Part{}, false
	}
	if m == nil {
		writeError(w, http.StatusForbidden, "not a member of this part's team", "FORBIDDEN")
		return dbgen.Part{}, false
	}
	return part, true
}

// requirePartInTeamOrGlobal authorizes a catalog edit (distributor listing)
// reached from a /teams/{teamId} route. Distributor listings are objective
// catalog data, so global (shared) parts are allowed — but a team-scoped part
// belonging to a *different* team is not. Returns the part and true, else writes
// 403/404/500 and returns false.
func (h *Handler) requirePartInTeamOrGlobal(w http.ResponseWriter, r *http.Request, partID, teamID pgtype.UUID) (dbgen.Part, bool) {
	part, err := h.Queries.GetPart(r.Context(), partID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return dbgen.Part{}, false
	}
	if string(part.Scope) == "global" || part.TeamID == teamID {
		return part, true
	}
	writeError(w, http.StatusForbidden, "part belongs to another team", "FORBIDDEN")
	return dbgen.Part{}, false
}

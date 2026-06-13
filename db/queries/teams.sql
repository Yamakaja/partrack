-- Teams and membership queries.

-- name: ListAllTeams :many
-- All teams; used by site admins who have implicit access to every team.
SELECT id, name, description, created_at FROM teams ORDER BY name;

-- name: ListUserTeams :many
-- Teams the user is a member of, with their role in each and whether the
-- membership is directory-managed (vs a manual override).
SELECT
    t.id,
    t.name,
    t.description,
    t.created_at,
    tm.role,
    tm.directory_managed
FROM team_memberships tm
JOIN teams t ON t.id = tm.team_id
WHERE tm.user_id = $1
ORDER BY t.name;

-- name: GetTeam :one
SELECT id, name, description, created_at
FROM teams
WHERE id = $1;

-- name: CreateTeam :one
INSERT INTO teams (name, description)
VALUES ($1, $2)
RETURNING id, name, description, created_at;

-- name: UpdateTeam :one
UPDATE teams
SET
    name        = COALESCE(sqlc.narg('name'),        name),
    description = COALESCE(sqlc.narg('description'), description)
WHERE id = sqlc.arg('id')
RETURNING id, name, description, created_at;

-- name: CreateTeamMembership :one
INSERT INTO team_memberships (team_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING id, team_id, user_id, role, created_at;

-- name: GetTeamByName :one
-- Resolves a team name (as configured in LDAP group_mappings) to its id.
SELECT id, name, description, created_at
FROM teams
WHERE name = $1;

-- name: SyncDirectoryMembership :exec
-- Grants or updates a directory-managed membership. A new row is flagged
-- directory_managed; an existing row is only updated when it is still
-- directory-managed, so a manual override (directory_managed = FALSE) is never
-- reverted by group sync.
INSERT INTO team_memberships (team_id, user_id, role, directory_managed)
VALUES ($1, $2, $3, TRUE)
ON CONFLICT (team_id, user_id) DO UPDATE
    SET role = EXCLUDED.role
    WHERE team_memberships.directory_managed = TRUE;

-- name: GetMembershipDetail :one
-- Role + directory-managed flag for one (team, user); used by LDAP diagnostics
-- to show whether a manual override would be preserved.
SELECT role, directory_managed
FROM team_memberships
WHERE team_id = $1
  AND user_id = $2;

-- name: RemoveDirectoryMembership :exec
-- Removes a membership only if it is still directory-managed; manual overrides
-- (and manually added memberships) survive a user leaving a directory group.
DELETE FROM team_memberships
WHERE team_id = $1
  AND user_id = $2
  AND directory_managed = TRUE;

-- name: ListTeamMembers :many
SELECT
    tm.id,
    tm.user_id,
    tm.role,
    tm.directory_managed,
    tm.created_at AS joined_at,
    u.display_name,
    u.email
FROM team_memberships tm
JOIN users u ON u.id = tm.user_id
WHERE tm.team_id = $1
ORDER BY u.display_name;

-- name: UpdateTeamMemberRole :one
-- A manual role change pins the membership (directory_managed = FALSE) so a
-- later directory sync cannot revert the admin's override.
UPDATE team_memberships
SET role = $1, directory_managed = FALSE
WHERE team_id = $2
  AND user_id = $3
RETURNING id, team_id, user_id, role, created_at;

-- name: RemoveTeamMember :exec
DELETE FROM team_memberships
WHERE team_id = $1
  AND user_id = $2;

-- name: GetUserByID :one
SELECT id, display_name, email, is_active, created_at
FROM users
WHERE id = $1;

-- name: DeactivateUser :exec
UPDATE users
SET is_active = FALSE
WHERE id = $1;

-- name: CheckTeamMembership :one
-- Returns pgx.ErrNoRows when the user is not a member of the team.
SELECT id FROM team_memberships WHERE team_id = $1 AND user_id = $2 LIMIT 1;

-- name: SearchUsers :many
-- Case-insensitive substring search on display_name or email.
-- Returns at most 10 active users. Enforces a minimum 2-character query
-- in the handler to prevent full-table scans.
SELECT id, display_name, email
FROM users
WHERE is_active = TRUE
  AND (display_name ILIKE '%' || $1 || '%' OR email ILIKE '%' || $1 || '%')
ORDER BY display_name
LIMIT 10;

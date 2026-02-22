-- Teams and membership queries.

-- name: ListUserTeams :many
-- Teams the user is a member of, with their role in each.
SELECT
    t.id,
    t.name,
    t.description,
    t.created_at,
    tm.role
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

-- name: ListTeamMembers :many
SELECT
    tm.id,
    tm.user_id,
    tm.role,
    tm.created_at AS joined_at,
    u.display_name,
    u.email
FROM team_memberships tm
JOIN users u ON u.id = tm.user_id
WHERE tm.team_id = $1
ORDER BY u.display_name;

-- name: UpdateTeamMemberRole :one
UPDATE team_memberships
SET role = $1
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

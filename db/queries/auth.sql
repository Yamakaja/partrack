-- Auth queries: session/token lookup used by middleware, plus session lifecycle.

-- name: GetUserBySessionToken :one
-- Used by Authenticate middleware. Validates token hash, expiry, and user active flag.
-- Also touches last_seen_at so idle session detection works.
SELECT
    u.id,
    u.display_name,
    u.email,
    u.is_active,
    u.created_at,
    s.id       AS session_id,
    s.expires_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.expires_at > NOW()
  AND u.is_active = TRUE;

-- name: UpdateSessionLastSeen :exec
UPDATE sessions
SET last_seen_at = NOW()
WHERE id = $1;

-- name: GetUserByAPIToken :one
-- Used by Authenticate middleware for Bearer API tokens.
SELECT
    u.id,
    u.display_name,
    u.email,
    u.is_active,
    u.created_at,
    t.id AS token_id
FROM api_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1
  AND u.is_active = TRUE
  AND (t.expires_at IS NULL OR t.expires_at > NOW());

-- name: UpdateAPITokenLastUsed :exec
UPDATE api_tokens
SET last_used_at = NOW()
WHERE id = $1;

-- name: GetTeamMembership :one
-- Used by RequireTeamRole middleware.
SELECT id, team_id, user_id, role, created_at
FROM team_memberships
WHERE team_id = $1
  AND user_id = $2;

-- name: GetUserByEmail :one
SELECT id, display_name, email, is_active, created_at
FROM users
WHERE email = $1;

-- name: GetUserCredential :one
SELECT id, user_id, password_hash, updated_at
FROM user_credentials
WHERE user_id = $1;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent, ip_address)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, token_hash, created_at, expires_at, last_seen_at, user_agent, ip_address;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: GetOIDCIdentity :one
SELECT id, user_id, provider, subject, provider_email, linked_at
FROM oidc_identities
WHERE provider = $1
  AND subject = $2;

-- name: CreateUser :one
INSERT INTO users (display_name, email)
VALUES ($1, $2)
RETURNING id, display_name, email, is_active, created_at;

-- name: CreateOIDCIdentity :one
INSERT INTO oidc_identities (user_id, provider, subject, provider_email)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, provider, subject, provider_email, linked_at;

-- name: GetUserWithMemberships :one
SELECT
    u.id,
    u.display_name,
    u.email,
    u.is_active,
    u.created_at
FROM users u
WHERE u.id = $1;

-- name: ListUserTeamMemberships :many
SELECT
    tm.id,
    tm.team_id,
    tm.role,
    tm.created_at,
    t.name  AS team_name
FROM team_memberships tm
JOIN teams t ON t.id = tm.team_id
WHERE tm.user_id = $1
ORDER BY t.name;

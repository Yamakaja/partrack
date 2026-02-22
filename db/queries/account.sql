-- Account management queries: sessions, API tokens, user preferences.

-- name: ListSessionsByUser :many
SELECT id, user_id, created_at, expires_at, last_seen_at, user_agent, ip_address
FROM sessions
WHERE user_id = $1
  AND expires_at > NOW()
ORDER BY last_seen_at DESC;

-- name: DeleteSessionByID :one
-- Only deletes if the session belongs to the requesting user.
-- Returns ErrNoRows when the session doesn't exist or belongs to a different user.
DELETE FROM sessions
WHERE id = $1
  AND user_id = $2
RETURNING id;

-- name: ListAPITokensByUser :many
SELECT id, user_id, name, created_at, last_used_at, expires_at
FROM api_tokens
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, name, created_at, last_used_at, expires_at;

-- name: DeleteAPITokenByID :exec
DELETE FROM api_tokens
WHERE id = $1
  AND user_id = $2;

-- name: GetPreferences :one
SELECT preferences
FROM user_preferences
WHERE user_id = $1;

-- name: UpsertPreferences :one
INSERT INTO user_preferences (user_id, preferences, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE
SET preferences = EXCLUDED.preferences,
    updated_at  = NOW()
RETURNING preferences;

-- name: GetOAuthToken :one
SELECT id, team_id, distributor_id, access_token, refresh_token, expires_at, updated_at, account_id
FROM oauth_tokens
WHERE team_id = $1 AND distributor_id = $2;

-- name: UpsertOAuthToken :one
INSERT INTO oauth_tokens (team_id, distributor_id, access_token, refresh_token, expires_at, updated_at, account_id)
VALUES ($1, $2, $3, $4, $5, now(), $6)
ON CONFLICT (team_id, distributor_id)
DO UPDATE SET access_token = $3, refresh_token = $4, expires_at = $5, updated_at = now(), account_id = $6
RETURNING id, expires_at;

-- name: DeleteOAuthToken :exec
DELETE FROM oauth_tokens WHERE team_id = $1 AND distributor_id = $2;

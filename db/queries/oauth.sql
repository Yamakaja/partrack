-- name: GetOAuthToken :one
SELECT id, team_id, user_id, distributor_id, access_token, refresh_token, expires_at, updated_at, account_id
FROM oauth_tokens
WHERE team_id = $1 AND user_id = $2 AND distributor_id = $3;

-- name: UpsertOAuthToken :one
INSERT INTO oauth_tokens (team_id, user_id, distributor_id, access_token, refresh_token, expires_at, updated_at, account_id)
VALUES ($1, $2, $3, $4, $5, $6, now(), $7)
ON CONFLICT (team_id, user_id, distributor_id)
DO UPDATE SET access_token = $4, refresh_token = $5, expires_at = $6, updated_at = now(), account_id = $7
RETURNING id, expires_at;

-- name: DeleteOAuthToken :exec
DELETE FROM oauth_tokens WHERE team_id = $1 AND user_id = $2 AND distributor_id = $3;

-- name: DeleteOAuthTokensForTeamDistributor :exec
DELETE FROM oauth_tokens WHERE team_id = $1 AND distributor_id = $2;

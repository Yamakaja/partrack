-- Distributor and distributor config queries.

-- name: ListDistributors :many
SELECT id, name, website, api_type FROM distributors ORDER BY name;

-- name: ListDistributorConfigs :many
SELECT
    dc.id,
    dc.distributor_id,
    d.name  AS distributor_name,
    dc.enabled,
    dc.preferred,
    dc.cache_duration_seconds,
    (dc.api_credentials IS NOT NULL) AS has_credentials
FROM distributor_configs dc
JOIN distributors d ON d.id = dc.distributor_id
WHERE dc.team_id = $1
ORDER BY d.name;

-- name: UpsertDistributorConfig :one
-- Updates api_credentials to the supplied value (pass NULL to clear).
INSERT INTO distributor_configs (team_id, distributor_id, enabled, preferred, cache_duration_seconds, api_credentials)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (team_id, distributor_id) DO UPDATE
SET enabled                = EXCLUDED.enabled,
    preferred              = EXCLUDED.preferred,
    cache_duration_seconds = EXCLUDED.cache_duration_seconds,
    api_credentials        = EXCLUDED.api_credentials
RETURNING id, team_id, distributor_id, enabled, preferred, cache_duration_seconds,
          (api_credentials IS NOT NULL) AS has_credentials;

-- name: UpsertDistributorConfigKeepCredentials :one
-- Leaves api_credentials unchanged when the caller omits the field.
INSERT INTO distributor_configs (team_id, distributor_id, enabled, preferred, cache_duration_seconds)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (team_id, distributor_id) DO UPDATE
SET enabled                = EXCLUDED.enabled,
    preferred              = EXCLUDED.preferred,
    cache_duration_seconds = EXCLUDED.cache_duration_seconds
RETURNING id, team_id, distributor_id, enabled, preferred, cache_duration_seconds,
          (api_credentials IS NOT NULL) AS has_credentials;

-- name: DeleteDistributorConfig :exec
DELETE FROM distributor_configs
WHERE team_id = $1 AND distributor_id = $2;

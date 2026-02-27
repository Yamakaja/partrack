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

-- name: ListEnabledDistributorConfigsForTeam :many
-- Returns enabled configs where the distributor has an API integration,
-- ordered preferred-first so enrichment priority is deterministic.
SELECT
    dc.distributor_id,
    d.name             AS distributor_name,
    d.api_type,
    dc.api_credentials,
    dc.cache_duration_seconds
FROM distributor_configs dc
JOIN distributors d ON d.id = dc.distributor_id
WHERE dc.team_id = $1 AND dc.enabled = TRUE AND d.api_type IS NOT NULL
ORDER BY dc.preferred DESC, d.name;

-- name: GetDistributorConfigForSearch :one
-- Returns api_type and credentials for a specific enabled distributor config.
-- Used by the distributor search handler.
SELECT d.api_type, dc.api_credentials
FROM distributor_configs dc
JOIN distributors d ON d.id = dc.distributor_id
WHERE dc.team_id = $1 AND dc.distributor_id = $2 AND dc.enabled = TRUE;

-- name: GetListingBySKUAndDistributorName :one
SELECT dl.id, dl.part_id
FROM distributor_listings dl
JOIN distributors d ON d.id = dl.distributor_id
WHERE dl.sku = $1
  AND d.name ILIKE $2
LIMIT 1;

-- name: GetListingBySKUAndDistributorID :one
SELECT id, part_id
FROM distributor_listings
WHERE sku = $1 AND distributor_id = $2
LIMIT 1;


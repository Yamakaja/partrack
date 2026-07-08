-- Distributor and distributor config queries.

-- name: ListDistributors :many
SELECT id, name, website, api_type FROM distributors ORDER BY name;

-- name: GetDistributor :one
SELECT id, name, website, api_type FROM distributors WHERE id = $1;

-- name: GetDistributorByApiType :one
SELECT id, name, website, api_type FROM distributors WHERE api_type = $1;

-- name: ListDistributorConfigs :many
SELECT
    dc.id,
    dc.distributor_id,
    d.name  AS distributor_name,
    d.api_type,
    dc.enabled,
    dc.preferred,
    dc.priority,
    dc.cache_duration_seconds,
    (dc.api_credentials IS NOT NULL) AS has_credentials,
    dc.api_credentials
FROM distributor_configs dc
JOIN distributors d ON d.id = dc.distributor_id
WHERE dc.team_id = $1
ORDER BY dc.priority ASC NULLS LAST, d.name;

-- name: GetDistributorConfigRaw :one
-- Returns api_type and raw credentials for a team+distributor config.
-- Used by the upsert handler to merge-preserve existing secret field values.
SELECT d.api_type, dc.api_credentials
FROM distributor_configs dc
JOIN distributors d ON d.id = dc.distributor_id
WHERE dc.team_id = $1 AND dc.distributor_id = $2;

-- name: ClearTeamPreferredDistributor :exec
-- Clears preferred on all distributor configs for a team.
-- Call this inside a transaction before setting a new preferred distributor.
UPDATE distributor_configs
SET preferred = FALSE
WHERE team_id = $1 AND preferred = TRUE;

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
    dc.cache_duration_seconds,
    dc.preferred
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

-- name: GetTeamSourcingSettings :one
-- Returns the team's sourcing strategy row, if one exists.
SELECT team_id, strategy, cost_tolerance_pct, cost_tolerance_abs, created_at, updated_at
FROM team_sourcing_settings
WHERE team_id = $1;

-- name: UpsertTeamSourcingSettings :one
INSERT INTO team_sourcing_settings (team_id, strategy, cost_tolerance_pct, cost_tolerance_abs)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id) DO UPDATE
SET strategy           = EXCLUDED.strategy,
    cost_tolerance_pct = EXCLUDED.cost_tolerance_pct,
    cost_tolerance_abs = EXCLUDED.cost_tolerance_abs,
    updated_at         = now()
RETURNING team_id, strategy, cost_tolerance_pct, cost_tolerance_abs, created_at, updated_at;

-- name: ClearTeamDistributorPriorities :exec
-- Resets the ranked preference for a team. Call before applying a new ordering.
UPDATE distributor_configs
SET priority = NULL, preferred = FALSE
WHERE team_id = $1 AND (priority IS NOT NULL OR preferred = TRUE);

-- name: SetDistributorPriority :exec
-- Sets one distributor's rank. `preferred` is kept in sync (rank 1 = the legacy
-- preferred distributor) so enrichment ordering and existing readers stay coherent.
UPDATE distributor_configs
SET priority = @priority, preferred = (@priority::int = 1)
WHERE team_id = @team_id AND distributor_id = @distributor_id;

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


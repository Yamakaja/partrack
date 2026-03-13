-- Parameter registry and distributor mapping queries.

-- name: ListParameterDefinitions :many
SELECT * FROM parameter_definitions ORDER BY display_name;

-- name: GetParameterDefinition :one
SELECT * FROM parameter_definitions WHERE id = $1;

-- name: GetParameterBySlug :one
SELECT * FROM parameter_definitions WHERE slug = $1;

-- name: CreateParameterDefinition :one
INSERT INTO parameter_definitions (slug, display_name, unit, unit_symbol, value_type)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateParameterDefinition :one
UPDATE parameter_definitions
SET slug         = $2,
    display_name = $3,
    unit         = $4,
    unit_symbol  = $5,
    value_type   = $6
WHERE id = $1
RETURNING *;

-- name: GetMappedParameter :one
-- Returns the partrack parameter definition for a given distributor parameter ID.
-- Returns no rows if the mapping does not exist or has not been linked (parameter_id IS NULL).
SELECT pd.*
FROM distributor_parameter_mappings dpm
JOIN parameter_definitions pd ON pd.id = dpm.parameter_id
WHERE dpm.distributor_id = $1
  AND dpm.distributor_param_id = $2
  AND dpm.parameter_id IS NOT NULL;

-- name: UpsertDistributorParamMapping :one
-- Inserts a new mapping row or updates the distributor_param_name if it already exists.
-- Used during enrichment to register newly-seen distributor parameters for admin review.
INSERT INTO distributor_parameter_mappings (distributor_id, distributor_param_id, distributor_param_name)
VALUES ($1, $2, $3)
ON CONFLICT (distributor_id, distributor_param_id)
DO UPDATE SET distributor_param_name = EXCLUDED.distributor_param_name
RETURNING *;

-- name: ListDistributorParamMappings :many
-- Lists all mappings for a distributor. Pass NULL to list all distributors.
-- When unmapped_only = true, only rows with parameter_id IS NULL (and not ignored) are returned.
-- sample_values contains up to 5 distinct raw values seen for this parameter across all parts.
SELECT
    dpm.id, dpm.distributor_id, dpm.distributor_param_id, dpm.distributor_param_name, dpm.parameter_id, dpm.ignored,
    dist.name AS distributor_name,
    COUNT(pp.id) AS part_count,
    (
        SELECT array_agg(v ORDER BY v)
        FROM (
            SELECT DISTINCT pp2.value AS v
            FROM part_parameters pp2
            WHERE pp2.source_distributor_id = dpm.distributor_id
              AND pp2.name = dpm.distributor_param_name
            LIMIT 5
        ) sub
    ) AS sample_values
FROM distributor_parameter_mappings dpm
JOIN distributors dist ON dist.id = dpm.distributor_id
LEFT JOIN part_parameters pp
    ON pp.source_distributor_id = dpm.distributor_id
    AND pp.name = dpm.distributor_param_name
WHERE (sqlc.narg('distributor_id')::uuid IS NULL OR dpm.distributor_id = sqlc.narg('distributor_id'))
  AND (NOT sqlc.arg('unmapped_only') OR (dpm.parameter_id IS NULL AND dpm.ignored = FALSE))
GROUP BY dpm.id, dist.name
ORDER BY part_count DESC, dpm.distributor_param_name;

-- name: SetMappingParameterID :exec
UPDATE distributor_parameter_mappings SET parameter_id = $2 WHERE id = $1;

-- name: SetMappingIgnored :exec
UPDATE distributor_parameter_mappings SET ignored = $2 WHERE id = $1;

-- name: FindPartsByParameterRange :many
-- Returns distinct parts where a specific parameter falls within a numeric range.
-- Used by advanced part search (3.1). Both bounds are inclusive.
SELECT DISTINCT p.*
FROM parts p
JOIN part_parameters pp ON pp.part_id = p.id
WHERE pp.parameter_id = $1
  AND pp.numeric_value >= $2
  AND pp.numeric_value <= $3
  AND (p.scope = 'global' OR p.team_id = $4)
ORDER BY pp.numeric_value;

-- name: FindPartIDsByParameterRange :many
-- Returns distinct part IDs where a specific parameter falls within a numeric range.
-- Used to pre-filter parts for parameter-based search. Both bounds are inclusive.
-- Pass NULL for either bound to omit that constraint (open-ended range).
SELECT DISTINCT pp.part_id
FROM part_parameters pp
JOIN parts p ON p.id = pp.part_id
WHERE pp.parameter_id = $1
  AND (sqlc.narg('min_val')::float8 IS NULL OR pp.numeric_value >= sqlc.narg('min_val'))
  AND (sqlc.narg('max_val')::float8 IS NULL OR pp.numeric_value <= sqlc.narg('max_val'))
  AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = $2));

-- name: FindPartIDsByParameterValues :many
-- Returns distinct part IDs where a specific parameter matches any of the given values.
-- Used to pre-filter parts for enum/text parameter-based search.
SELECT DISTINCT pp.part_id
FROM part_parameters pp
JOIN parts p ON p.id = pp.part_id
WHERE pp.parameter_id = $1
  AND pp.value = ANY($2::text[])
  AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = $3));

-- name: GetCategoryParameters :many
-- Returns parameter definitions present for parts in a category (and all descendant categories),
-- including numeric value ranges and distinct enum/text values found across those parts.
-- Range-type parameters are excluded. Optional team_id scopes to global + team-owned parts.
WITH RECURSIVE cat_tree AS (
    SELECT cat.id FROM categories cat WHERE cat.id = $1
    UNION ALL
    SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
)
SELECT
    pd.id,
    pd.slug,
    pd.display_name,
    pd.unit,
    pd.unit_symbol,
    pd.value_type,
    MIN(CASE WHEN pd.value_type = 'numeric' THEN pp.numeric_value END) AS min_numeric,
    MAX(CASE WHEN pd.value_type = 'numeric' THEN pp.numeric_value END) AS max_numeric,
    array_remove(
        array_agg(DISTINCT CASE WHEN pd.value_type IN ('enum', 'text') AND pp.value IS NOT NULL THEN pp.value END),
        NULL
    ) AS enum_values
FROM part_parameters pp
JOIN parts p ON p.id = pp.part_id
JOIN parameter_definitions pd ON pd.id = pp.parameter_id
WHERE p.category_id IN (SELECT ct2.id FROM cat_tree ct2)
  AND pp.parameter_id IS NOT NULL
  AND pd.value_type != 'range'
  AND (sqlc.narg('team_id')::uuid IS NULL OR p.scope = 'global' OR p.team_id = sqlc.narg('team_id'))
GROUP BY pd.id, pd.slug, pd.display_name, pd.unit, pd.unit_symbol, pd.value_type
ORDER BY pd.display_name;

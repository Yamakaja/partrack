-- name: ListStorageLocations :many
SELECT id, team_id, parent_id, label, description, grid_config
FROM storage_locations
WHERE team_id = $1
ORDER BY label;

-- name: GetStorageLocation :one
SELECT id, team_id, parent_id, label, description, grid_config
FROM storage_locations
WHERE id = $1 AND team_id = $2;

-- name: ListStorageLocationAncestors :many
-- Returns the location itself plus all ancestors, ordered root-first (depth DESC).
WITH RECURSIVE ancestors AS (
    SELECT sl.id, sl.team_id, sl.parent_id, sl.label, sl.description, sl.grid_config, 0 AS depth
    FROM storage_locations sl
    WHERE sl.id = sqlc.arg('location_id') AND sl.team_id = sqlc.arg('team_id')
    UNION ALL
    SELECT s.id, s.team_id, s.parent_id, s.label, s.description, s.grid_config, a.depth + 1
    FROM storage_locations s
    JOIN ancestors a ON s.id = a.parent_id
)
SELECT id, team_id, parent_id, label, description, grid_config FROM ancestors
ORDER BY depth DESC;

-- name: CreateStorageLocation :one
INSERT INTO storage_locations (team_id, parent_id, label, description, grid_config)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, team_id, parent_id, label, description, grid_config;

-- name: UpdateStorageLocation :one
UPDATE storage_locations
SET label       = COALESCE(sqlc.narg('label'), label),
    description = COALESCE(sqlc.narg('description'), description),
    grid_config = CASE WHEN sqlc.arg('set_grid_config')::bool
                       THEN sqlc.narg('grid_config')
                       ELSE grid_config END
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, team_id, parent_id, label, description, grid_config;

-- name: DeleteStorageLocation :exec
DELETE FROM storage_locations WHERE id = $1 AND team_id = $2;

-- name: CountInventoryEntriesForLocation :one
SELECT COUNT(*) FROM inventory_entries
WHERE storage_location_id = $1;

-- name: ListInventoryByLocation :many
-- Returns inventory entries at the given location or any descendant location.
WITH RECURSIVE descendants AS (
    SELECT sl.id FROM storage_locations sl
    WHERE sl.id = sqlc.arg('location_id') AND sl.team_id = sqlc.arg('team_id')
    UNION ALL
    SELECT s.id FROM storage_locations s
    JOIN descendants d ON s.parent_id = d.id
)
SELECT
    ie.id, ie.team_id, ie.part_id,
    p.mpn AS part_mpn, p.description AS part_description,
    ie.quantity_on_hand, ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label,
    ie.preferred_listing_id,
    ie.minimum_stock_quantity, ie.reorder_quantity
FROM inventory_entries ie
JOIN parts p ON p.id = ie.part_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE ie.storage_location_id IN (SELECT id FROM descendants)
  AND ie.team_id = sqlc.arg('team_id')
ORDER BY p.mpn NULLS LAST, ie.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountInventoryByLocation :one
WITH RECURSIVE descendants AS (
    SELECT sl.id FROM storage_locations sl
    WHERE sl.id = sqlc.arg('location_id') AND sl.team_id = sqlc.arg('team_id')
    UNION ALL
    SELECT s.id FROM storage_locations s
    JOIN descendants d ON s.parent_id = d.id
)
SELECT COUNT(*)
FROM inventory_entries ie
WHERE ie.storage_location_id IN (SELECT id FROM descendants)
  AND ie.team_id = sqlc.arg('team_id');

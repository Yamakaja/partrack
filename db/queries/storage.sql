-- name: ListStorageLocations :many
SELECT id, team_id, label, description
FROM storage_locations
WHERE team_id = $1
ORDER BY label;

-- name: CreateStorageLocation :one
INSERT INTO storage_locations (team_id, label, description)
VALUES ($1, $2, $3)
RETURNING id, team_id, label, description;

-- name: UpdateStorageLocation :one
UPDATE storage_locations
SET label       = COALESCE(sqlc.narg('label'), label),
    description = COALESCE(sqlc.narg('description'), description)
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, team_id, label, description;

-- name: DeleteStorageLocation :exec
DELETE FROM storage_locations WHERE id = $1 AND team_id = $2;

-- name: CountInventoryEntriesForLocation :one
SELECT COUNT(*) FROM inventory_entries
WHERE storage_location_id = $1;

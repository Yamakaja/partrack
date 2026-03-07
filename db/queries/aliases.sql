-- Part alias queries.

-- name: InsertPartAlias :exec
INSERT INTO part_aliases (part_id, alias) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: GetPartAliases :many
SELECT alias FROM part_aliases WHERE part_id = $1 ORDER BY alias;

-- name: DeletePartAliases :exec
DELETE FROM part_aliases WHERE part_id = $1;

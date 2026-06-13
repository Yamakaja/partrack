-- Batch resolvers for note object-link manifests. Each takes a team_id + a list
-- of UUIDs and returns the display fields needed to render a link chip. All are
-- team-scoped so a note can only resolve objects its team can see.

-- name: GetProjectsByIDs :many
SELECT id, name, status
FROM projects
WHERE team_id = $1 AND id = ANY(sqlc.arg('ids')::uuid[]);

-- name: GetBOMRevisionsByIDs :many
SELECT br.id, br.label, br.project_id, pr.name AS project_name
FROM bom_revisions br
JOIN projects pr ON pr.id = br.project_id
WHERE pr.team_id = $1 AND br.id = ANY(sqlc.arg('ids')::uuid[]);

-- name: GetOrdersByIDs :many
SELECT o.id, o.status, o.purchase_order_number, o.external_reference, d.name AS distributor_name
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
WHERE o.team_id = $1 AND o.id = ANY(sqlc.arg('ids')::uuid[]);

-- name: GetBuildsByIDs :many
SELECT b.id, b.status, b.quantity, pr.name AS project_name, br.label AS revision_label
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
WHERE b.team_id = $1 AND b.id = ANY(sqlc.arg('ids')::uuid[]);

-- name: GetStorageLocationsByIDs :many
SELECT id, label
FROM storage_locations
WHERE team_id = $1 AND id = ANY(sqlc.arg('ids')::uuid[]);

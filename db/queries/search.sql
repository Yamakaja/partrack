-- name: SearchParts :many
SELECT p.id, p.mpn, p.manufacturer, p.description, p.short_description, p.lifecycle_status
FROM parts p
WHERE (p.scope = 'global' OR p.team_id = @team_id)
  AND (
    p.mpn ILIKE '%' || @q || '%'
    OR p.manufacturer ILIKE '%' || @q || '%'
    OR p.description ILIKE '%' || @q || '%'
    OR p.short_description ILIKE '%' || @q || '%'
    OR EXISTS (SELECT 1 FROM part_aliases pa WHERE pa.part_id = p.id AND pa.alias ILIKE '%' || @q || '%')
  )
ORDER BY p.mpn
LIMIT @lim;

-- name: SearchProjects :many
SELECT id, name, description, status
FROM projects
WHERE team_id = @team_id
  AND (name ILIKE '%' || @q || '%' OR description ILIKE '%' || @q || '%')
ORDER BY name
LIMIT @lim;

-- name: SearchOrders :many
SELECT o.id, o.status, o.purchase_order_number, o.external_reference, d.name AS distributor_name
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
WHERE o.team_id = @team_id
  AND (
    o.purchase_order_number ILIKE '%' || @q || '%'
    OR o.external_reference ILIKE '%' || @q || '%'
    OR d.name ILIKE '%' || @q || '%'
  )
ORDER BY o.created_at DESC
LIMIT @lim;

-- name: SearchBuilds :many
SELECT b.id, b.status, b.quantity,
       pr.name AS project_name, br.label AS revision_label
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
WHERE b.team_id = @team_id
  AND (pr.name ILIKE '%' || @q || '%' OR br.label ILIKE '%' || @q || '%')
ORDER BY b.created_at DESC
LIMIT @lim;

-- name: SearchStorageLocations :many
SELECT id, label, description
FROM storage_locations
WHERE team_id = @team_id
  AND (label ILIKE '%' || @q || '%' OR description ILIKE '%' || @q || '%')
ORDER BY label
LIMIT @lim;

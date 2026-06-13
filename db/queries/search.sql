-- name: SearchParts :many
SELECT p.id, p.mpn, p.manufacturer, p.description, p.short_description, p.lifecycle_status
FROM parts p
WHERE (p.scope = 'global' OR p.team_id = @team_id)
  AND p.deleted_at IS NULL
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

-- name: SearchBOMRevisions :many
SELECT br.id, br.label, br.project_id, pr.name AS project_name
FROM bom_revisions br
JOIN projects pr ON pr.id = br.project_id
WHERE pr.team_id = @team_id
  AND (pr.name ILIKE '%' || @q || '%' OR br.label ILIKE '%' || @q || '%')
ORDER BY pr.name, br.label
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
WITH RECURSIVE tree AS (
    SELECT sl.id, sl.label, sl.description, sl.parent_id,
           NULL::text AS ancestor_path
    FROM storage_locations sl
    WHERE sl.team_id = sqlc.arg('team_id') AND sl.parent_id IS NULL
    UNION ALL
    SELECT s.id, s.label, s.description, s.parent_id,
           CASE WHEN t.ancestor_path IS NULL THEN t.label
                ELSE t.ancestor_path || ' > ' || t.label
           END
    FROM storage_locations s
    JOIN tree t ON s.parent_id = t.id
)
SELECT id, label, description, ancestor_path
FROM tree
WHERE label ILIKE '%' || sqlc.arg('q') || '%' OR description ILIKE '%' || sqlc.arg('q') || '%'
ORDER BY label
LIMIT sqlc.arg('lim');

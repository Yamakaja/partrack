-- name: CreateScanAuditLog :one
INSERT INTO scan_audit_log (
    team_id, user_id, session_id, event_type,
    entity_type, entity_id, entity_name, quantity,
    parsed_sku, parsed_distributor_id, parsed_mpn, parsed_manufacturer,
    metadata
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8,
    $9, $10, $11, $12,
    $13
)
RETURNING id, team_id, user_id, session_id, event_type,
          entity_type, entity_id, entity_name, quantity,
          parsed_sku, parsed_distributor_id, parsed_mpn, parsed_manufacturer,
          metadata, created_at;

-- name: ListScanAuditLog :many
SELECT sal.id, sal.team_id, sal.user_id, sal.session_id, sal.event_type,
       sal.entity_type, sal.entity_id, sal.entity_name, sal.quantity,
       sal.parsed_sku, sal.parsed_distributor_id, sal.parsed_mpn, sal.parsed_manufacturer,
       sal.metadata, sal.created_at,
       p.manufacturer      AS part_manufacturer,
       p.short_description AS part_short_description,
       p.image_url         AS part_image_url,
       c.display_name      AS part_category_name,
       ie.id               AS inventory_entry_id,
       ie.quantity_on_hand AS inventory_quantity_on_hand
FROM scan_audit_log sal
LEFT JOIN parts p              ON sal.entity_type = 'part' AND sal.entity_id = p.id
LEFT JOIN categories c         ON p.category_id = c.id
LEFT JOIN inventory_entries ie ON sal.entity_type = 'part'
                               AND sal.entity_id = ie.part_id
                               AND ie.team_id = sal.team_id
WHERE sal.team_id = @team_id
  AND sal.user_id = @user_id
  AND (
    array_length(sqlc.slice('event_types')::text[], 1) IS NULL
    OR sal.event_type = ANY(sqlc.slice('event_types')::text[])
  )
  AND (
    sqlc.narg('cursor_time')::timestamptz IS NULL
    OR (sal.created_at, sal.id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY sal.created_at DESC, sal.id DESC
LIMIT sqlc.arg('limit');

-- name: ResolvePartNotFoundBySKU :exec
UPDATE scan_audit_log
SET    entity_id   = @part_id,
       entity_type = 'part',
       entity_name = @part_name
WHERE  event_type          = 'part_not_found'
  AND  parsed_sku          = @parsed_sku
  AND  parsed_distributor_id = @parsed_distributor_id
  AND  entity_id IS NULL;

-- name: ResolvePartNotFoundByMPN :exec
UPDATE scan_audit_log
SET    entity_id   = @part_id,
       entity_type = 'part',
       entity_name = @part_name
WHERE  event_type           = 'part_not_found'
  AND  parsed_mpn           = @parsed_mpn
  AND  parsed_manufacturer  = @parsed_manufacturer
  AND  entity_id IS NULL;

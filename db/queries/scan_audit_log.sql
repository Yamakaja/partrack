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
SELECT id, team_id, user_id, session_id, event_type,
       entity_type, entity_id, entity_name, quantity,
       parsed_sku, parsed_distributor_id, parsed_mpn, parsed_manufacturer,
       metadata, created_at
FROM scan_audit_log
WHERE team_id = @team_id
  AND user_id = @user_id
  AND (
    array_length(sqlc.slice('event_types')::text[], 1) IS NULL
    OR event_type = ANY(sqlc.slice('event_types')::text[])
  )
  AND (
    sqlc.narg('cursor_time')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
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

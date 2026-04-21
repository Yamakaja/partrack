-- name: InsertBulkScanSession :one
INSERT INTO bulk_scan_sessions (user_id, team_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetActiveBulkScanSessionForUser :one
SELECT *
FROM bulk_scan_sessions
WHERE user_id = $1
  AND closed_at IS NULL
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: CloseBulkScanSession :exec
UPDATE bulk_scan_sessions
SET closed_at = NOW()
WHERE id = $1
  AND user_id = $2
  AND closed_at IS NULL;

-- name: SoftDeleteBulkScanSession :exec
UPDATE bulk_scan_sessions
SET deleted_at = NOW()
WHERE id = $1
  AND team_id = $2
  AND user_id = $3
  AND deleted_at IS NULL;

-- name: UpdateBulkScanSessionLastItemAt :exec
UPDATE bulk_scan_sessions
SET last_item_at = NOW()
WHERE id = $1;

-- name: InsertBulkScanItem :one
INSERT INTO bulk_scan_items (session_id, barcode, parsed_fields)
VALUES ($1, $2, $3)
ON CONFLICT (session_id, barcode) WHERE status NOT IN ('dismissed', 'reviewed') DO NOTHING
RETURNING *;

-- name: GetActiveBulkScanItemByBarcode :one
SELECT *
FROM bulk_scan_items
WHERE session_id = $1 AND barcode = $2
  AND status NOT IN ('dismissed', 'reviewed')
ORDER BY created_at DESC
LIMIT 1;

-- name: GetBulkScanItem :one
SELECT *
FROM bulk_scan_items
WHERE id = $1;

-- name: SetBulkScanItemEnrichmentJobID :exec
UPDATE bulk_scan_items
SET enrichment_job_id = $2
WHERE id = $1;

-- name: SetBulkScanItemInitialQty :exec
UPDATE bulk_scan_items
SET initial_qty = $2
WHERE id = $1
  AND session_id = $3;

-- name: SetBulkScanItemEnriched :one
UPDATE bulk_scan_items
SET status           = 'enriched',
    task_type        = $2,
    part_id          = $3,
    enrichment_data  = $4,
    enriched_at      = NOW(),
    enrichment_job_id = NULL,
    error_message    = NULL
WHERE id = $1
RETURNING *;

-- name: SetBulkScanItemError :exec
UPDATE bulk_scan_items
SET status            = 'error',
    error_message     = $2,
    enrichment_job_id = NULL
WHERE id = $1;

-- name: SetBulkScanItemStatus :one
UPDATE bulk_scan_items
SET status = $2
WHERE id   = $1
  AND session_id = $3
  AND status NOT IN ('reviewed', 'dismissed')
RETURNING *;

-- name: SetBulkScanItemRetry :one
UPDATE bulk_scan_items
SET status            = 'pending_enrichment',
    error_message     = NULL,
    enrichment_job_id = $2
WHERE id         = $1
  AND session_id = $3
  AND status     = 'error'
RETURNING *;

-- name: SetBulkScanItemDistributorRetryPending :one
UPDATE bulk_scan_items
SET status            = 'pending_enrichment',
    enrichment_job_id = $2
WHERE id     = $1
  AND status = 'enriched'
RETURNING *;

-- name: UpdateBulkScanItemEnrichmentData :one
UPDATE bulk_scan_items
SET enrichment_data = $2,
    status          = 'enriched',
    enriched_at     = NOW()
WHERE id = $1
  AND status IN ('enriched', 'pending_enrichment')
RETURNING *;

-- name: ListBulkScanSessionsForUser :many
SELECT
    s.*,
    COUNT(i.id) FILTER (WHERE i.status = 'pending_enrichment') AS count_pending,
    COUNT(i.id) FILTER (WHERE i.status = 'enriched')           AS count_enriched,
    COUNT(i.id) FILTER (WHERE i.status = 'error')              AS count_error,
    COUNT(i.id) FILTER (WHERE i.status = 'reviewed')           AS count_reviewed,
    COUNT(i.id) FILTER (WHERE i.status = 'dismissed')          AS count_dismissed
FROM bulk_scan_sessions s
LEFT JOIN bulk_scan_items i ON i.session_id = s.id
WHERE s.user_id  = $1
  AND s.team_id  = $2
  AND s.deleted_at IS NULL
GROUP BY s.id
HAVING COUNT(i.id) FILTER (WHERE i.status IN ('pending_enrichment', 'enriched', 'error')) > 0
    OR sqlc.arg('include_completed')::bool
ORDER BY s.created_at DESC;

-- name: GetBulkScanSessionCounts :one
SELECT
    COUNT(i.id) FILTER (WHERE i.status = 'pending_enrichment') AS count_pending,
    COUNT(i.id) FILTER (WHERE i.status = 'enriched')           AS count_enriched,
    COUNT(i.id) FILTER (WHERE i.status = 'error')              AS count_error,
    COUNT(i.id) FILTER (WHERE i.status = 'reviewed')           AS count_reviewed,
    COUNT(i.id) FILTER (WHERE i.status = 'dismissed')          AS count_dismissed
FROM bulk_scan_sessions s
LEFT JOIN bulk_scan_items i ON i.session_id = s.id
WHERE s.id      = $1
  AND s.team_id = $2
  AND s.deleted_at IS NULL
GROUP BY s.id;

-- name: GetBulkScanSessionBadgeCount :one
SELECT COALESCE(SUM(cnt), 0)::int AS badge_count
FROM (
    SELECT COUNT(i.id) AS cnt
    FROM bulk_scan_sessions s
    JOIN bulk_scan_items i ON i.session_id = s.id
    WHERE s.user_id    = $1
      AND s.team_id    = $2
      AND s.deleted_at IS NULL
      AND i.status IN ('pending_enrichment', 'enriched', 'error')
    GROUP BY s.id
) sub;

-- name: ListBulkScanItems :many
SELECT
    i.*,
    p.mpn           AS part_mpn,
    p.manufacturer  AS part_manufacturer,
    p.description   AS part_description
FROM bulk_scan_items i
LEFT JOIN parts p ON p.id = i.part_id
WHERE i.session_id = $1
ORDER BY i.created_at ASC;

-- name: GetBulkScanSession :one
SELECT *
FROM bulk_scan_sessions
WHERE id       = $1
  AND team_id  = $2
  AND deleted_at IS NULL;

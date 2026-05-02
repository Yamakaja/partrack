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
INSERT INTO bulk_scan_items
    (session_id, barcode, status, task_type, part_id, parsed_fields, initial_qty, resolve_item_id)
VALUES (@session_id, @barcode, @status, @task_type, @part_id, @parsed_fields, @initial_qty, @resolve_item_id)
ON CONFLICT (session_id, barcode) WHERE status NOT IN ('dismissed', 'reviewed') DO NOTHING
RETURNING *;

-- name: SetBulkScanItemResolveID :exec
-- Called after the resolve item is created; links the scan item to its resolve item.
UPDATE bulk_scan_items
SET resolve_item_id = @resolve_item_id
WHERE id = @id;

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

-- name: SetBulkScanItemInitialQty :exec
UPDATE bulk_scan_items
SET initial_qty = $2
WHERE id = $1
  AND session_id = $3;

-- name: SetBulkScanItemReviewed :one
-- Marks a scan item reviewed and optionally sets part_id.
-- Returns 0 rows if already terminal (concurrent-safe).
UPDATE bulk_scan_items
SET status  = 'reviewed',
    part_id = COALESCE(@part_id, part_id)
WHERE id     = @id
  AND status = 'pending_review'
RETURNING *;

-- name: SetBulkScanItemDismissed :one
UPDATE bulk_scan_items
SET status = 'dismissed'
WHERE id     = @id
  AND status = 'pending_review'
RETURNING *;

-- name: GetBulkScanItemWithResolveData :one
-- Fetches scan item + resolve item data in a single query for the review UI.
SELECT
    bsi.*,
    pri.status          AS resolve_status,
    pri.task_type       AS resolve_task_type,
    pri.enrichment_data AS resolve_enrichment_data,
    pri.error_message   AS resolve_error_message,
    pri.enriched_at     AS resolve_enriched_at
FROM bulk_scan_items bsi
LEFT JOIN part_resolve_items pri ON pri.id = bsi.resolve_item_id
WHERE bsi.id = @id;

-- name: ListBulkScanItemsWithResolveData :many
-- Returns all items for a session with their resolve item data joined.
SELECT
    bsi.*,
    p.mpn           AS part_mpn,
    p.manufacturer  AS part_manufacturer,
    p.description   AS part_description,
    pri.id          AS resolve_item_id_col,
    pri.status      AS resolve_status,
    pri.task_type   AS resolve_task_type,
    pri.enrichment_data AS resolve_enrichment_data,
    pri.error_message   AS resolve_error_message,
    pri.enriched_at     AS resolve_enriched_at
FROM bulk_scan_items bsi
LEFT JOIN parts p ON p.id = bsi.part_id
LEFT JOIN part_resolve_items pri ON pri.id = bsi.resolve_item_id
WHERE bsi.session_id = @session_id
ORDER BY bsi.created_at;

-- name: GetBulkScanBadgeCount :one
-- Items awaiting user action: pending_review items where enrichment is done or not needed.
SELECT COUNT(*) FROM bulk_scan_items bsi
LEFT JOIN part_resolve_items pri ON pri.id = bsi.resolve_item_id
WHERE bsi.session_id IN (
    SELECT bss.id FROM bulk_scan_sessions bss
    WHERE bss.team_id = @team_id AND bss.deleted_at IS NULL
      AND bss.user_id = @user_id
)
AND bsi.status = 'pending_review'
AND (bsi.resolve_item_id IS NULL OR pri.status IN ('enriched', 'failed'));

-- name: CountBulkScanSessionItems :one
SELECT
    COUNT(*) FILTER (WHERE bsi.status = 'pending_review') AS pending_count,
    COUNT(*) FILTER (WHERE bsi.status = 'reviewed')       AS reviewed_count,
    COUNT(*) FILTER (WHERE bsi.status = 'dismissed')      AS dismissed_count,
    COUNT(*) FILTER (
        WHERE bsi.status = 'pending_review'
          AND (bsi.resolve_item_id IS NULL OR pri.status IN ('enriched','failed'))
    ) AS ready_count
FROM bulk_scan_items bsi
LEFT JOIN part_resolve_items pri ON pri.id = bsi.resolve_item_id
WHERE bsi.session_id = @session_id;

-- name: ListBulkScanSessionsForUser :many
SELECT
    s.*,
    COUNT(i.id) FILTER (
        WHERE i.status = 'pending_review'
          AND i.resolve_item_id IS NOT NULL
          AND pri.status NOT IN ('enriched', 'failed')
    )                                                                                                                    AS count_pending,
    COUNT(i.id) FILTER (WHERE i.status = 'reviewed')                                                                    AS count_reviewed,
    COUNT(i.id) FILTER (WHERE i.status = 'dismissed')                                                                   AS count_dismissed,
    COUNT(i.id) FILTER (
        WHERE i.status = 'pending_review'
          AND (i.resolve_item_id IS NULL OR pri.status IN ('enriched','failed'))
    )                                                                                                                    AS count_ready
FROM bulk_scan_sessions s
LEFT JOIN bulk_scan_items i ON i.session_id = s.id
LEFT JOIN part_resolve_items pri ON pri.id = i.resolve_item_id
WHERE s.user_id  = $1
  AND s.team_id  = $2
  AND s.deleted_at IS NULL
GROUP BY s.id
HAVING COUNT(i.id) FILTER (WHERE i.status = 'pending_review') > 0
    OR sqlc.arg('include_completed')::bool
ORDER BY s.created_at DESC;

-- name: GetBulkScanSession :one
SELECT *
FROM bulk_scan_sessions
WHERE id       = $1
  AND team_id  = $2
  AND deleted_at IS NULL;

-- name: UpdateBulkScanItemTaskType :exec
-- Called by handlePartResolve when source_type='bulk_scan_item' to propagate
-- the resolved task_type and part_id back to the scan item.
UPDATE bulk_scan_items
SET task_type = @task_type,
    part_id   = @part_id
WHERE id = @id;

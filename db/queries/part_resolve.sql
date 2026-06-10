-- name: GetStalePendingResolveItems :many
-- Returns pending items older than the given cutoff so the worker can re-enqueue them.
-- Includes the team_id and created_by so the job payload can be reconstructed.
SELECT id, team_id, created_by
FROM part_resolve_items
WHERE status = 'pending'
  AND created_at < @cutoff;

-- name: CreatePartResolveItem :one
INSERT INTO part_resolve_items (team_id, source_type, source_id, input_fields, created_by)
VALUES (@team_id, @source_type, @source_id, @input_fields, @created_by)
RETURNING *;

-- name: GetPartResolveItem :one
SELECT * FROM part_resolve_items WHERE id = @id;

-- name: GetPartBySKUAndDistributorIDWithInventory :one
-- Exact distributor UUID + SKU lookup. More precise than the name-based ILIKE variant
-- used by the barcode scanner because the caller already has the distributor UUID.
SELECT
    p.id,
    ie.id AS inventory_entry_id
FROM distributor_listings dl
JOIN parts p ON p.id = dl.part_id
    AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = @team_id))
    AND p.deleted_at IS NULL
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = @team_id
WHERE dl.sku = @sku
  AND dl.distributor_id = @distributor_id
ORDER BY (p.scope = 'team' AND p.team_id = @team_id) DESC
LIMIT 1;

-- name: UpdatePartResolveItemEnriched :one
UPDATE part_resolve_items
SET    status           = 'enriched',
       task_type        = @task_type,
       enrichment_data  = @enrichment_data,
       resolved_part_id = @resolved_part_id,
       enriched_at      = NOW()
WHERE  id = @id
RETURNING *;

-- name: UpdatePartResolveItemFailed :exec
UPDATE part_resolve_items
SET    status        = 'failed',
       error_message = @error_message
WHERE  id = @id;

-- name: UpdatePartResolveItemRetryPending :exec
-- Resets an enriched or failed item back to pending so the job can re-run.
UPDATE part_resolve_items
SET    status = 'pending'
WHERE  id = @id
  AND  status IN ('enriched', 'failed');


-- name: CommitPartResolveItem :one
-- Atomic test-and-set. Only succeeds when status = 'enriched'.
-- Returns 0 rows if already committed or in the wrong state; caller returns 409.
UPDATE part_resolve_items
SET    status           = 'committed',
       resolved_part_id = @resolved_part_id,
       committed_at     = NOW()
WHERE  id     = @id
  AND  status = 'enriched'
RETURNING *;

-- name: DeleteOldPartResolveItems :exec
-- Invoked by the 30-day cleanup goroutine.
DELETE FROM part_resolve_items
WHERE  status IN ('committed', 'failed')
  AND  created_at < NOW() - INTERVAL '30 days';

-- name: GetListingIDByPartAndSKU :one
-- Resolves a listing row ID from part + distributor + SKU.
-- Used at commit time to wire up preferred_listing_id on the inventory entry.
SELECT id FROM distributor_listings
WHERE  part_id        = @part_id
  AND  distributor_id = @distributor_id
  AND  sku            = @sku
LIMIT 1;

-- name: ListBuilds :many
SELECT
    b.id, b.project_id, pr.name AS project_name,
    b.bom_revision_id, br.label AS bom_revision_label,
    b.quantity, b.status,
    b.assembled_at, b.created_at, b.notes, b.created_by,
    u_assembled.display_name AS assembled_by_display_name,
    u_created.display_name   AS created_by_display_name
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
LEFT JOIN users u_assembled ON u_assembled.id = b.assembled_by
LEFT JOIN users u_created   ON u_created.id   = b.created_by
WHERE b.team_id = @team_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR b.project_id = sqlc.narg('project_id'))
  AND (array_length(sqlc.slice('statuses')::text[], 1) IS NULL OR b.status = ANY(sqlc.slice('statuses')::text[]))
  AND (sqlc.narg('created_by_filter')::uuid IS NULL OR b.created_by = sqlc.narg('created_by_filter'))
ORDER BY
  CASE WHEN sqlc.narg('sort_by')::text = 'project_name'       AND sqlc.narg('sort_dir')::text = 'asc'  THEN pr.name       END ASC  NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'project_name'       AND sqlc.narg('sort_dir')::text = 'desc' THEN pr.name       END DESC NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'bom_revision_label' AND sqlc.narg('sort_dir')::text = 'asc'  THEN br.label      END ASC  NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'bom_revision_label' AND sqlc.narg('sort_dir')::text = 'desc' THEN br.label      END DESC NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'status'             AND sqlc.narg('sort_dir')::text = 'asc'  THEN b.status      END ASC  NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'status'             AND sqlc.narg('sort_dir')::text = 'desc' THEN b.status      END DESC NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'quantity'           AND sqlc.narg('sort_dir')::text = 'asc'  THEN b.quantity    END ASC  NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'quantity'           AND sqlc.narg('sort_dir')::text = 'desc' THEN b.quantity    END DESC NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'created_at'         AND sqlc.narg('sort_dir')::text = 'asc'  THEN b.created_at  END ASC  NULLS LAST,
  CASE WHEN sqlc.narg('sort_by')::text = 'created_at'         AND sqlc.narg('sort_dir')::text = 'desc' THEN b.created_at  END DESC NULLS LAST,
  b.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountBuilds :one
SELECT COUNT(*) FROM builds
WHERE team_id = @team_id
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (array_length(sqlc.slice('statuses')::text[], 1) IS NULL OR status = ANY(sqlc.slice('statuses')::text[]))
  AND (sqlc.narg('created_by_filter')::uuid IS NULL OR created_by = sqlc.narg('created_by_filter'));

-- name: CreateBuildPlan :one
INSERT INTO builds (team_id, project_id, bom_revision_id, quantity, status, notes, created_at, created_by)
VALUES ($1, $2, $3, $4, 'draft', $5, NOW(), $6)
RETURNING id, team_id, project_id, bom_revision_id, quantity, status, notes, created_at, created_by,
          assembled_at, assembled_by, assembled_quantity, excluded_from_cart, excluded_designators;

-- name: GetBuild :one
SELECT
    b.id, b.team_id, b.project_id, pr.name AS project_name,
    b.bom_revision_id, br.label AS bom_revision_label,
    b.quantity, b.status, b.notes,
    b.assembled_at, b.assembled_quantity, b.excluded_from_cart,
    b.excluded_designators,
    b.created_at, b.created_by,
    b.selections_checked_at,
    u_assembled.display_name AS assembled_by_display_name,
    u_created.display_name   AS created_by_display_name
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
LEFT JOIN users u_assembled ON u_assembled.id = b.assembled_by
LEFT JOIN users u_created   ON u_created.id   = b.created_by
WHERE b.id = $1 AND b.team_id = $2;

-- name: GetBuildForUpdate :one
SELECT id, team_id, project_id, bom_revision_id, quantity, status, notes,
       assembled_at, assembled_by, assembled_quantity, created_at, created_by, excluded_from_cart,
       excluded_designators, selections_checked_at
FROM builds
WHERE id = $1 AND team_id = $2
FOR UPDATE;

-- name: UpdateBuildStatus :exec
UPDATE builds SET status = $2 WHERE id = $1 AND team_id = $3;

-- name: UpdateBuildAssembled :one
UPDATE builds
SET status             = $2,
    assembled_quantity = $3,
    assembled_at       = $4,
    assembled_by       = $5
WHERE id = $1 AND team_id = $6
RETURNING id, status, assembled_quantity, assembled_at;

-- name: UpdateBuildQuantityAndNotes :one
UPDATE builds
SET quantity = COALESCE(sqlc.narg('quantity'), quantity),
    notes    = COALESCE(sqlc.narg('notes'), notes)
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
  AND status IN ('draft', 'pending')
RETURNING id, quantity, notes, status;

-- name: SetBuildExcludedFromCart :exec
UPDATE builds
SET excluded_from_cart = $2
WHERE id = $1 AND team_id = $3 AND status = 'pending';

-- name: GetPendingBuildsForTeam :many
SELECT b.id, b.team_id, b.project_id, pr.name AS project_name,
       b.bom_revision_id, br.label AS revision_label,
       b.quantity AS build_quantity, b.excluded_from_cart, b.excluded_designators
FROM builds b
JOIN projects pr      ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
WHERE b.status = 'pending' AND b.team_id = $1
ORDER BY b.created_at;

-- name: CreateBuildOrder :exec
INSERT INTO build_orders (build_id, order_id) VALUES ($1, $2);

-- name: GetBuildOrderIDs :many
SELECT order_id FROM build_orders WHERE build_id = $1;

-- name: CreateBuildPartAllocation :one
INSERT INTO build_part_allocations (build_id, part_id, quantity_needed, quantity_reserved, quantity_received)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, build_id, part_id, quantity_needed, quantity_reserved, quantity_received;

-- name: GetBuildPartAllocations :many
SELECT bpa.id, bpa.build_id, bpa.part_id, bpa.quantity_needed, bpa.quantity_reserved, bpa.quantity_received,
       p.mpn AS part_mpn
FROM build_part_allocations bpa
JOIN parts p ON p.id = bpa.part_id
WHERE bpa.build_id = $1;

-- name: UpsertBuildPartAllocation :one
INSERT INTO build_part_allocations (build_id, part_id, quantity_needed, quantity_reserved, quantity_received)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (build_id, part_id) DO UPDATE
    SET quantity_needed   = EXCLUDED.quantity_needed,
        quantity_reserved = EXCLUDED.quantity_reserved,
        quantity_received = EXCLUDED.quantity_received
RETURNING id, build_id, part_id, quantity_needed, quantity_reserved, quantity_received;

-- name: IncrementBuildPartAllocationReceived :exec
UPDATE build_part_allocations
SET quantity_received = quantity_received + $3
WHERE build_id = $1 AND part_id = $2;

-- name: DeleteBuildPartAllocations :exec
DELETE FROM build_part_allocations WHERE build_id = $1;

-- name: CheckBuildReadiness :one
SELECT NOT EXISTS (
    SELECT 1 FROM build_part_allocations
    WHERE build_id = $1
      AND (quantity_reserved + quantity_received) < quantity_needed
) AS is_ready;

-- name: GetLinkedBuildAllocationsForOrderPart :many
SELECT bpa.build_id, bpa.part_id, bpa.quantity_needed, bpa.quantity_reserved, bpa.quantity_received
FROM build_part_allocations bpa
JOIN build_orders bo ON bo.build_id = bpa.build_id
WHERE bo.order_id = $1 AND bpa.part_id = $2;

-- name: SumReservedForPart :one
SELECT COALESCE(SUM(bpa.quantity_reserved), 0)::int AS total_reserved
FROM build_part_allocations bpa
JOIN builds b ON bpa.build_id = b.id
WHERE bpa.part_id = $1
  AND b.team_id   = $2
  AND b.status    NOT IN ('assembled', 'cancelled');

-- name: GetLinkedBuildIDsForOrder :many
SELECT build_id FROM build_orders WHERE order_id = $1;

-- name: GetLinkedBuildsForOrder :many
SELECT
    b.id, b.project_id, pr.name AS project_name,
    b.bom_revision_id, br.label AS bom_revision_label,
    b.quantity, b.status,
    b.created_at,
    u_created.display_name AS created_by_display_name,
    COUNT(DISTINCT oli.id)::int AS total_line_items,
    COUNT(DISTINCT CASE WHEN bpa.quantity_received >= bpa.quantity_needed THEN oli.id END)::int AS fulfilled_line_items
FROM build_orders bo
JOIN builds b ON b.id = bo.build_id
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
LEFT JOIN users u_created ON u_created.id = b.created_by
JOIN order_line_items oli ON oli.order_id = bo.order_id
JOIN distributor_listings dl ON dl.id = oli.listing_id
JOIN build_part_allocations bpa ON bpa.build_id = bo.build_id AND bpa.part_id = dl.part_id
WHERE bo.order_id = $1
GROUP BY b.id, b.project_id, pr.name, b.bom_revision_id, br.label,
         b.quantity, b.status, b.created_at, u_created.display_name
ORDER BY b.created_at;

-- name: GetReservedQtysForParts :many
SELECT bpa.part_id,
       COALESCE(SUM(bpa.quantity_reserved), 0)::int AS total_reserved
FROM build_part_allocations bpa
JOIN builds b ON bpa.build_id = b.id
WHERE bpa.part_id = ANY(@part_ids::uuid[])
  AND b.team_id   = @team_id
  AND b.status    NOT IN ('assembled', 'cancelled')
GROUP BY bpa.part_id;

-- name: GetOrderedBuildsNeedingPart :many
SELECT b.id
FROM build_part_allocations bpa
JOIN builds b ON bpa.build_id = b.id
WHERE bpa.part_id = @part_id
  AND b.team_id   = @team_id
  AND b.status    = 'ordered';

-- name: UpdateBuildPartReservation :exec
UPDATE build_part_allocations
SET quantity_reserved = @quantity_reserved
WHERE build_id = @build_id AND part_id = @part_id;

-- name: DecrementBuildQuantity :one
UPDATE builds
SET quantity = quantity - @assembled_quantity
WHERE id = @id AND team_id = @team_id
RETURNING id, quantity, status;

-- name: AdjustBuildPartAllocationAfterPartialCheckout :exec
UPDATE build_part_allocations
SET quantity_reserved = GREATEST(0, quantity_reserved - LEAST(quantity_reserved, sqlc.arg('consumed'))),
    quantity_received = GREATEST(0, quantity_received - GREATEST(0, sqlc.arg('consumed') - quantity_reserved)),
    quantity_needed   = quantity_needed - sqlc.arg('consumed')
WHERE build_id = sqlc.arg('build_id') AND part_id = sqlc.arg('part_id');

-- name: GetActiveAllocationsForPart :many
SELECT
    bpa.build_id, bpa.quantity_needed, bpa.quantity_reserved, bpa.quantity_received,
    b.status AS build_status, b.quantity AS build_quantity,
    pr.name AS project_name, pr.id AS project_id
FROM build_part_allocations bpa
JOIN builds b ON b.id = bpa.build_id
JOIN projects pr ON pr.id = b.project_id
WHERE bpa.part_id = @part_id
  AND b.team_id   = @team_id
  AND b.status    NOT IN ('assembled', 'cancelled')
ORDER BY b.created_at;

-- name: ListBuildDeductions :many
SELECT
    sa.id, sa.inventory_entry_id, sa.delta,
    ie.part_id, p.mpn AS part_mpn
FROM stock_adjustments sa
JOIN inventory_entries ie ON ie.id = sa.inventory_entry_id
JOIN parts p ON p.id = ie.part_id
WHERE sa.build_id = $1
ORDER BY sa.id;

-- name: CreateBuildStockAdjustment :one
INSERT INTO stock_adjustments (inventory_entry_id, delta, reason, created_by, build_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, inventory_entry_id, delta, adjusted_at, reason, created_by, build_id;

-- name: GetBuildPipeline :many
SELECT b.id, b.project_id, b.status, b.quantity,
       b.created_at, b.notes, b.created_by, b.excluded_from_cart,
       b.bom_revision_id,
       pr.name AS project_name,
       br.label AS bom_revision_label,
       u.display_name AS created_by_display_name
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
LEFT JOIN users u ON u.id = b.created_by
WHERE b.team_id = $1
  AND b.status NOT IN ('assembled', 'cancelled')
ORDER BY b.created_at DESC;


-- name: DeleteBuildOrderLink :exec
DELETE FROM build_orders WHERE build_id = @build_id AND order_id = @order_id;

-- name: DeleteBuildOrdersByOrder :exec
DELETE FROM build_orders WHERE order_id = $1;

-- name: SetBuildExcludedDesignators :exec
UPDATE builds SET excluded_designators = @designators WHERE id = @build_id AND team_id = @team_id;

-- name: GetBuildLineItemOverrides :many
SELECT build_id, bom_line_item_id, override_type
FROM build_line_item_overrides
WHERE build_id = $1;

-- name: GetBuildLineItemOverridesForBuilds :many
SELECT build_id, bom_line_item_id, override_type
FROM build_line_item_overrides
WHERE build_id = ANY(@build_ids::uuid[]);

-- name: UpsertBuildLineItemOverride :exec
INSERT INTO build_line_item_overrides (build_id, bom_line_item_id, override_type)
VALUES ($1, $2, $3)
ON CONFLICT (build_id, bom_line_item_id) DO UPDATE SET override_type = EXCLUDED.override_type;

-- name: DeleteBuildLineItemOverride :exec
DELETE FROM build_line_item_overrides WHERE build_id = $1 AND bom_line_item_id = $2;

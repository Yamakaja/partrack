-- name: ListInventory :many
SELECT
    ie.id, ie.team_id, ie.part_id,
    p.mpn AS part_mpn, p.description AS part_description,
    ie.quantity_on_hand, ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label,
    ie.preferred_listing_id,
    ie.minimum_stock_quantity, ie.reorder_quantity
FROM inventory_entries ie
JOIN parts p ON p.id = ie.part_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE ie.team_id = $1
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('storage_location_id')::uuid IS NULL
       OR ie.storage_location_id = sqlc.narg('storage_location_id'))
  AND (NOT sqlc.arg('below_min_stock')::bool
       OR (ie.minimum_stock_quantity IS NOT NULL
           AND ie.quantity_on_hand < ie.minimum_stock_quantity))
ORDER BY p.mpn NULLS LAST, ie.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountInventory :one
SELECT COUNT(*) FROM inventory_entries ie
JOIN parts p ON p.id = ie.part_id
WHERE ie.team_id = $1
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('storage_location_id')::uuid IS NULL
       OR ie.storage_location_id = sqlc.narg('storage_location_id'))
  AND (NOT sqlc.arg('below_min_stock')::bool
       OR (ie.minimum_stock_quantity IS NOT NULL
           AND ie.quantity_on_hand < ie.minimum_stock_quantity));

-- name: CreateInventoryEntry :one
INSERT INTO inventory_entries (team_id, part_id, storage_location_id, preferred_listing_id,
                               minimum_stock_quantity, reorder_quantity)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, team_id, part_id, quantity_on_hand, storage_location_id,
          preferred_listing_id, minimum_stock_quantity, reorder_quantity, storage_coordinate;

-- name: GetInventoryEntry :one
SELECT
    ie.id, ie.team_id, ie.part_id,
    ie.quantity_on_hand, ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label,
    ie.preferred_listing_id,
    ie.minimum_stock_quantity, ie.reorder_quantity
FROM inventory_entries ie
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE ie.id = $1 AND ie.team_id = $2;

-- name: GetInventoryQuantityForUpdate :one
-- Locks the entry row and returns the current on-hand for a compare-and-set stock
-- adjustment. Must run inside the adjustment transaction.
SELECT quantity_on_hand
FROM inventory_entries
WHERE id = $1 AND team_id = $2
FOR UPDATE;

-- name: UpdateInventoryEntry :one
UPDATE inventory_entries
SET storage_location_id    = CASE WHEN sqlc.arg('set_storage_location_id')::bool
                                  THEN sqlc.narg('storage_location_id')::uuid
                                  ELSE storage_location_id    END,
    storage_coordinate     = CASE WHEN sqlc.arg('set_storage_coordinate')::bool
                                  THEN sqlc.narg('storage_coordinate')::integer[]
                                  ELSE storage_coordinate     END,
    preferred_listing_id   = CASE WHEN sqlc.arg('set_preferred_listing_id')::bool
                                  THEN sqlc.narg('preferred_listing_id')::uuid
                                  ELSE preferred_listing_id   END,
    minimum_stock_quantity = CASE WHEN sqlc.arg('set_minimum_stock_quantity')::bool
                                  THEN sqlc.narg('minimum_stock_quantity')
                                  ELSE minimum_stock_quantity END,
    reorder_quantity       = CASE WHEN sqlc.arg('set_reorder_quantity')::bool
                                  THEN sqlc.narg('reorder_quantity')
                                  ELSE reorder_quantity       END
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, team_id, part_id, quantity_on_hand, storage_location_id,
          preferred_listing_id, minimum_stock_quantity, reorder_quantity, storage_coordinate;

-- name: ListAdjustments :many
SELECT
    sa.id, sa.delta, sa.adjusted_at, sa.reason,
    sa.created_by,
    u.display_name AS created_by_display_name,
    sa.order_line_item_id,
    oli.order_id AS source_order_id,
    sa.build_id, sa.general_checkout_id
FROM stock_adjustments sa
JOIN users u ON u.id = sa.created_by
LEFT JOIN order_line_items oli ON oli.id = sa.order_line_item_id
WHERE sa.inventory_entry_id = $1
ORDER BY sa.adjusted_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAdjustments :one
SELECT COUNT(*) FROM stock_adjustments WHERE inventory_entry_id = $1;

-- name: CreateStockAdjustment :one
WITH adj AS (
    INSERT INTO stock_adjustments (inventory_entry_id, delta, reason, created_by)
    VALUES ($1, $2, $3, $4)
    RETURNING id, inventory_entry_id, delta, adjusted_at, reason, created_by,
              order_line_item_id, build_id, general_checkout_id
), updated AS (
    UPDATE inventory_entries
    SET quantity_on_hand = quantity_on_hand + adj.delta
    FROM adj
    WHERE inventory_entries.id = adj.inventory_entry_id
    RETURNING inventory_entries.quantity_on_hand
)
SELECT adj.id, adj.inventory_entry_id, adj.delta, adj.adjusted_at, adj.reason,
       adj.created_by, adj.order_line_item_id, adj.build_id, adj.general_checkout_id,
       updated.quantity_on_hand AS new_quantity_on_hand
FROM adj
JOIN updated ON TRUE;

-- name: ReplenishPreview :many
SELECT
    ie.id AS inventory_entry_id,
    ie.part_id,
    p.mpn AS part_mpn,
    ie.quantity_on_hand,
    ie.minimum_stock_quantity,
    ie.minimum_stock_quantity - ie.quantity_on_hand AS shortfall,
    ie.reorder_quantity,
    COALESCE(ie.reorder_quantity, ie.minimum_stock_quantity - ie.quantity_on_hand) AS quantity_to_add_to_cart,
    ie.preferred_listing_id
FROM inventory_entries ie
JOIN parts p ON p.id = ie.part_id
WHERE ie.team_id = $1
  AND ie.minimum_stock_quantity IS NOT NULL
  AND ie.quantity_on_hand < ie.minimum_stock_quantity
ORDER BY p.mpn NULLS LAST;

-- name: GetInventoryEntryByPartID :one
SELECT id, quantity_on_hand, storage_location_id, storage_coordinate
FROM inventory_entries
WHERE team_id = $1 AND part_id = $2;

-- name: GetPreferredListingDistributorByPartID :one
-- Returns the distributor_id of the team's preferred listing for a part.
-- Returns no rows if the inventory entry has no preferred listing set.
SELECT dl.distributor_id
FROM inventory_entries ie
JOIN distributor_listings dl ON dl.id = ie.preferred_listing_id
WHERE ie.team_id = $1 AND ie.part_id = $2;

-- name: GetInventoryEntryForUpdate :one
SELECT id, team_id, part_id, quantity_on_hand, storage_location_id,
       preferred_listing_id, minimum_stock_quantity, reorder_quantity
FROM inventory_entries
WHERE id = $1
FOR UPDATE;

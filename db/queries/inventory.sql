-- name: ListInventory :many
SELECT
    ie.id, ie.team_id, ie.part_id,
    p.mpn AS part_mpn, p.description AS part_description,
    ie.quantity_on_hand, ie.storage_location_id,
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
          preferred_listing_id, minimum_stock_quantity, reorder_quantity;

-- name: GetInventoryEntry :one
SELECT
    ie.id, ie.team_id, ie.part_id,
    ie.quantity_on_hand, ie.storage_location_id,
    sl.label AS storage_location_label,
    ie.preferred_listing_id,
    ie.minimum_stock_quantity, ie.reorder_quantity
FROM inventory_entries ie
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE ie.id = $1 AND ie.team_id = $2;

-- name: UpdateInventoryEntry :one
UPDATE inventory_entries
SET storage_location_id    = CASE WHEN sqlc.arg('set_storage_location_id')::bool
                                  THEN sqlc.narg('storage_location_id')::uuid
                                  ELSE storage_location_id    END,
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
          preferred_listing_id, minimum_stock_quantity, reorder_quantity;

-- name: ListAdjustments :many
SELECT
    sa.id, sa.delta, sa.adjusted_at, sa.reason,
    sa.created_by,
    u.display_name AS created_by_display_name,
    sa.order_line_item_id, sa.build_id, sa.general_checkout_id
FROM stock_adjustments sa
JOIN users u ON u.id = sa.created_by
WHERE sa.inventory_entry_id = $1
ORDER BY sa.adjusted_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAdjustments :one
SELECT COUNT(*) FROM stock_adjustments WHERE inventory_entry_id = $1;

-- name: CreateStockAdjustment :one
INSERT INTO stock_adjustments (inventory_entry_id, delta, reason, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id, inventory_entry_id, delta, adjusted_at, reason, created_by,
          order_line_item_id, build_id, general_checkout_id;

-- name: ApplyStockDelta :one
UPDATE inventory_entries
SET quantity_on_hand = quantity_on_hand + $2
WHERE id = $1
RETURNING quantity_on_hand;

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
SELECT id, quantity_on_hand
FROM inventory_entries
WHERE team_id = $1 AND part_id = $2;

-- name: GetInventoryEntryForUpdate :one
SELECT id, team_id, part_id, quantity_on_hand, storage_location_id,
       preferred_listing_id, minimum_stock_quantity, reorder_quantity
FROM inventory_entries
WHERE id = $1
FOR UPDATE;

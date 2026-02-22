-- name: ListCheckouts :many
SELECT
    gc.id, gc.performed_at, gc.reason,
    u.display_name AS performed_by_display_name,
    COUNT(gcli.id) AS item_count
FROM general_checkouts gc
JOIN users u ON u.id = gc.performed_by
LEFT JOIN general_checkout_line_items gcli ON gcli.general_checkout_id = gc.id
WHERE gc.team_id = $1
GROUP BY gc.id, u.display_name
ORDER BY gc.performed_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountCheckouts :one
SELECT COUNT(*) FROM general_checkouts WHERE team_id = $1;

-- name: CreateCheckout :one
INSERT INTO general_checkouts (team_id, reason, performed_by)
VALUES ($1, $2, $3)
RETURNING id, team_id, performed_at, reason, performed_by;

-- name: CreateCheckoutLineItem :one
INSERT INTO general_checkout_line_items (general_checkout_id, part_id, quantity)
VALUES ($1, $2, $3)
RETURNING id, general_checkout_id, part_id, quantity;

-- name: GetCheckout :one
SELECT
    gc.id, gc.team_id, gc.performed_at, gc.reason,
    u.display_name AS performed_by_display_name
FROM general_checkouts gc
JOIN users u ON u.id = gc.performed_by
WHERE gc.id = $1 AND gc.team_id = $2;

-- name: GetCheckoutLineItems :many
SELECT
    gcli.part_id,
    p.mpn AS part_mpn,
    gcli.quantity,
    sa.id AS stock_adjustment_id
FROM general_checkout_line_items gcli
JOIN parts p ON p.id = gcli.part_id
LEFT JOIN inventory_entries ie ON ie.part_id = gcli.part_id AND ie.team_id = $2
LEFT JOIN stock_adjustments sa ON sa.general_checkout_id = $1 AND sa.inventory_entry_id = ie.id
WHERE gcli.general_checkout_id = $1
ORDER BY gcli.part_id;

-- name: CreateCheckoutStockAdjustment :one
INSERT INTO stock_adjustments (inventory_entry_id, delta, reason, created_by, general_checkout_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, inventory_entry_id, delta, adjusted_at, reason, created_by, general_checkout_id;

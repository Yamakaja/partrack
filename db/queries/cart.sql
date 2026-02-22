-- name: GetOrCreateCart :one
INSERT INTO shopping_carts (team_id)
VALUES ($1)
ON CONFLICT (team_id) DO UPDATE SET team_id = EXCLUDED.team_id
RETURNING id, team_id;

-- name: GetCartItems :many
SELECT
    cli.id,
    cli.part_id,
    p.mpn AS part_mpn,
    cli.quantity,
    cli.listing_id,
    dl.sku AS listing_sku,
    d.name AS listing_distributor_name,
    d.id AS listing_distributor_id,
    dl.unit_price AS listing_unit_price,
    cli.source,
    cli.source_bom_line_item_id
FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
JOIN parts p ON p.id = cli.part_id
LEFT JOIN distributor_listings dl ON dl.id = cli.listing_id
LEFT JOIN distributors d ON d.id = dl.distributor_id
WHERE sc.team_id = $1
ORDER BY p.mpn NULLS LAST, cli.id;

-- name: ClearCart :exec
DELETE FROM cart_line_items
WHERE shopping_cart_id = (SELECT id FROM shopping_carts WHERE team_id = $1);

-- name: UpsertCartItem :one
INSERT INTO cart_line_items (shopping_cart_id, part_id, quantity, listing_id, source, source_bom_line_item_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (shopping_cart_id, part_id)
DO UPDATE SET
    quantity   = cart_line_items.quantity + EXCLUDED.quantity,
    listing_id = COALESCE(EXCLUDED.listing_id, cart_line_items.listing_id)
RETURNING id, shopping_cart_id, part_id, quantity, listing_id, source, source_bom_line_item_id;

-- name: UpdateCartItemQuantity :one
UPDATE cart_line_items
SET quantity = $3
WHERE cart_line_items.id = $1
  AND shopping_cart_id = (SELECT sc.id FROM shopping_carts sc WHERE sc.team_id = $2)
RETURNING id, part_id, quantity, listing_id, source, source_bom_line_item_id;

-- name: UpdateCartItemListing :one
UPDATE cart_line_items
SET listing_id = $3
WHERE cart_line_items.id = $1
  AND shopping_cart_id = (SELECT sc.id FROM shopping_carts sc WHERE sc.team_id = $2)
RETURNING id, part_id, quantity, listing_id, source, source_bom_line_item_id;

-- name: DeleteCartItem :exec
DELETE FROM cart_line_items
WHERE cart_line_items.id = $1
  AND shopping_cart_id = (SELECT sc.id FROM shopping_carts sc WHERE sc.team_id = $2);

-- name: CountCartItemsWithoutListing :one
SELECT COUNT(*) FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
WHERE sc.team_id = $1 AND cli.listing_id IS NULL;

-- name: GetCartItemsForConvert :many
SELECT
    cli.id,
    cli.part_id,
    cli.quantity,
    cli.listing_id,
    dl.distributor_id,
    dl.sku,
    dl.packaging_type,
    dl.unit_price,
    dl.fixed_order_cost
FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
LEFT JOIN distributor_listings dl ON dl.id = cli.listing_id
WHERE sc.team_id = $1
ORDER BY dl.distributor_id NULLS LAST, cli.id;

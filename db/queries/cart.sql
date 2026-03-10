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
    p.description AS part_description,
    p.short_description AS part_short_description,
    p.manufacturer AS part_manufacturer,
    p.category AS part_category,
    p.package AS part_package,
    p.product_url AS part_product_url,
    p.datasheet_url AS part_datasheet_url,
    cli.quantity,
    cli.listing_id,
    dl.sku AS listing_sku,
    d.name AS listing_distributor_name,
    d.id AS listing_distributor_id,
    dl.unit_price AS listing_unit_price,
    dl.price_breaks AS listing_price_breaks,
    dl.fixed_order_cost AS listing_fixed_order_cost,
    dl.moq AS listing_moq,
    dl.packaging_type AS listing_packaging_type,
    dl.stock_available AS listing_stock_available,
    dl.last_refreshed_at AS listing_last_refreshed_at,
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
ON CONFLICT (shopping_cart_id, part_id, source)
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

-- name: CountCartItems :one
SELECT COUNT(*) FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
WHERE sc.team_id = $1;

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
    dl.fixed_order_cost,
    dl.price_breaks,
    dl.moq
FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
LEFT JOIN distributor_listings dl ON dl.id = cli.listing_id
WHERE sc.team_id = $1
ORDER BY dl.distributor_id NULLS LAST, cli.id;

-- name: GetPendingBuildsForTeamCart :many
-- Returns pending builds that are included in cart (excluded_from_cart = FALSE)
SELECT b.id, b.team_id, b.bom_revision_id, b.quantity AS build_quantity,
       br.label AS revision_label, pr.name AS project_name
FROM builds b
JOIN bom_revisions br ON br.id = b.bom_revision_id
JOIN projects pr       ON pr.id = br.project_id
WHERE b.team_id = $1
  AND b.status = 'pending'
  AND b.excluded_from_cart = FALSE
ORDER BY b.created_at;

-- name: ClearBomShortfallItems :exec
DELETE FROM cart_line_items
WHERE shopping_cart_id = $1 AND source = 'bom_shortfall';

-- name: GetInventoryQuantitiesForParts :many
SELECT ie.part_id, ie.quantity_on_hand, ie.minimum_stock_quantity, ie.reorder_quantity
FROM inventory_entries ie
WHERE ie.team_id = $1 AND ie.part_id = ANY($2::uuid[]);

-- name: GetCartItemByID :one
SELECT cli.id, cli.shopping_cart_id, cli.part_id, cli.quantity, cli.listing_id,
       cli.source, cli.source_bom_line_item_id
FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
WHERE cli.id = $1 AND sc.team_id = $2;

-- name: UpsertManualCartItem :one
INSERT INTO cart_line_items (shopping_cart_id, part_id, quantity, listing_id, source)
VALUES ($1, $2, $3, $4, 'manual')
ON CONFLICT (shopping_cart_id, part_id, source)
DO UPDATE SET
    quantity   = EXCLUDED.quantity,
    listing_id = COALESCE(EXCLUDED.listing_id, cart_line_items.listing_id)
RETURNING id, shopping_cart_id, part_id, quantity, listing_id, source, source_bom_line_item_id;

-- name: DeleteManualCartItemForPart :exec
DELETE FROM cart_line_items
WHERE shopping_cart_id = $1 AND part_id = $2 AND source = 'manual';

-- name: GetCartQuantityForPart :one
SELECT COALESCE(SUM(cli.quantity), 0)::int AS quantity_in_cart
FROM cart_line_items cli
JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id
WHERE sc.team_id = @team_id
  AND cli.part_id = @part_id
  AND (sqlc.narg('listing_id')::uuid IS NULL OR cli.listing_id = sqlc.narg('listing_id'));

-- name: GetListingsForCartResolution :many
SELECT
    dl.id              AS listing_id,
    dl.part_id,
    dl.distributor_id,
    d.name             AS distributor_name,
    dl.sku,
    dl.unit_price,
    dl.fixed_order_cost,
    dl.moq,
    dl.price_breaks,
    dl.packaging_type,
    dl.stock_available,
    dl.last_refreshed_at,
    dc.preferred
FROM distributor_listings dl
JOIN distributors d ON d.id = dl.distributor_id
JOIN distributor_configs dc
    ON dc.distributor_id = dl.distributor_id
    AND dc.team_id = @team_id
    AND dc.enabled = TRUE
WHERE dl.part_id = ANY(@part_ids::uuid[])
ORDER BY dl.part_id, dc.preferred DESC, d.name;

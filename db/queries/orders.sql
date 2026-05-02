-- name: ListReceivableOrdersForTeam :many
SELECT o.id, o.status, o.external_reference, o.purchase_order_number, o.notes,
       d.name AS distributor_name,
       COUNT(oli.id)::int                                                             AS item_count,
       COALESCE(SUM(oli.quantity_ordered), 0)::int                                   AS total_qty_ordered,
       COALESCE(SUM(oli.quantity_received), 0)::int                                  AS total_qty_received,
       COUNT(CASE WHEN oli.quantity_received >= oli.quantity_ordered THEN 1 END)::int AS items_fully_received,
       COALESCE(SUM(oli.quantity_ordered * oli.unit_price_snapshot
                    + oli.fixed_order_cost_snapshot), 0)::numeric                    AS total_value,
       o.created_by, u.display_name AS created_by_name, o.created_at
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
JOIN users u ON u.id = o.created_by
LEFT JOIN order_line_items oli ON oli.order_id = o.id
WHERE o.team_id = $1 AND o.status IN ('placed', 'partially_received')
GROUP BY o.id, d.name, o.created_by, u.display_name, o.created_at
ORDER BY o.created_at DESC LIMIT 50;

-- name: ListOrders :many
SELECT
    o.id, o.distributor_id, d.name AS distributor_name,
    o.status, o.external_reference, o.created_at,
    u.display_name AS created_by_display_name,
    COUNT(oli.id) AS item_count,
    SUM(oli.quantity_ordered * oli.unit_price_snapshot + oli.fixed_order_cost_snapshot)::numeric AS total_price
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
JOIN users u ON u.id = o.created_by
LEFT JOIN order_line_items oli ON oli.order_id = o.id
WHERE o.team_id = $1
  AND (sqlc.narg('status')::order_status IS NULL OR o.status = sqlc.narg('status'))
  AND (sqlc.narg('distributor_id')::uuid IS NULL OR o.distributor_id = sqlc.narg('distributor_id'))
GROUP BY o.id, d.name, u.display_name
ORDER BY o.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountOrders :one
SELECT COUNT(*) FROM orders
WHERE team_id = $1
  AND (sqlc.narg('status')::order_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('distributor_id')::uuid IS NULL OR distributor_id = sqlc.narg('distributor_id'));

-- name: GetOrder :one
SELECT
    o.id, o.team_id, o.distributor_id, d.name AS distributor_name,
    d.api_type AS distributor_api_type,
    o.status, o.submission_method, o.external_reference, o.external_ref_linked_at,
    o.received_at, o.notes, o.purchase_order_number, o.integrations, o.created_at,
    u.display_name AS created_by_display_name
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
JOIN users u ON u.id = o.created_by
WHERE o.id = $1 AND o.team_id = $2;

-- name: GetOrderLineItems :many
SELECT
    oli.id, oli.listing_id,
    dl.part_id,
    p.mpn AS part_mpn,
    p.manufacturer AS part_manufacturer,
    COALESCE(p.short_description, p.description) AS part_description,
    dl.sku, dl.packaging_type,
    oli.quantity_ordered, oli.quantity_received,
    oli.unit_price_snapshot, oli.fixed_order_cost_snapshot
FROM order_line_items oli
JOIN distributor_listings dl ON dl.id = oli.listing_id
JOIN parts p ON p.id = dl.part_id
WHERE oli.order_id = $1
ORDER BY oli.id;

-- name: CreateOrder :one
INSERT INTO orders (team_id, distributor_id, created_by)
VALUES ($1, $2, $3)
RETURNING id, team_id, distributor_id, status, created_by, created_at;

-- name: CreateOrderLineItem :one
INSERT INTO order_line_items (order_id, listing_id, quantity_ordered, unit_price_snapshot, fixed_order_cost_snapshot)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, order_id, listing_id, quantity_ordered, quantity_received, unit_price_snapshot, fixed_order_cost_snapshot;

-- name: UpdateOrderPurchaseOrderNumber :one
UPDATE orders
SET purchase_order_number = sqlc.narg('purchase_order_number')
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, purchase_order_number;

-- name: UpdateOrderNotes :one
UPDATE orders
SET notes = sqlc.narg('notes')
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, status, notes;

-- name: UpdateOrderLineItemQuantity :exec
UPDATE order_line_items
SET quantity_ordered = $2
WHERE order_line_items.id = $1
  AND order_id IN (SELECT o.id FROM orders o WHERE o.team_id = $3 AND o.status = 'draft');

-- name: GetOrderStatusForTeam :one
SELECT status FROM orders WHERE id = $1 AND team_id = $2;

-- name: CancelOrder :exec
UPDATE orders
SET status = 'cancelled'
WHERE id = $1 AND team_id = $2 AND status != 'received';

-- name: TransitionOrderStatus :exec
UPDATE orders SET status = $2 WHERE id = $1;

-- name: LinkOrderExternalReference :one
UPDATE orders
SET external_reference     = sqlc.arg('external_reference'),
    external_ref_linked_at = NOW(),
    status                 = 'placed'
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
  AND status IN ('draft', 'exported', 'submitted', 'placed')
RETURNING id, status, external_reference, external_ref_linked_at;

-- name: TransitionOrderToExported :exec
UPDATE orders
SET status = 'exported', submission_method = 'manual'
WHERE id = $1 AND team_id = $2 AND status IN ('draft', 'exported');

-- name: GetOrderLineItem :one
SELECT
    oli.id, oli.order_id, oli.listing_id,
    oli.quantity_ordered, oli.quantity_received,
    oli.unit_price_snapshot, oli.fixed_order_cost_snapshot,
    dl.part_id,
    o.team_id
FROM order_line_items oli
JOIN distributor_listings dl ON dl.id = oli.listing_id
JOIN orders o ON o.id = oli.order_id
WHERE oli.id = $1 AND o.team_id = $2;

-- name: ReceiveOrderLineItem :one
UPDATE order_line_items
SET quantity_received = quantity_received + $2
WHERE id = $1
RETURNING id, order_id, quantity_ordered, quantity_received;

-- name: GetOrderReceiptStatus :one
SELECT
    COUNT(*) AS total,
    COUNT(*) FILTER (WHERE quantity_received >= quantity_ordered) AS fully_received
FROM order_line_items WHERE order_id = $1;

-- name: CreateOrderStockAdjustment :one
WITH adj AS (
    INSERT INTO stock_adjustments (inventory_entry_id, delta, reason, created_by, order_line_item_id)
    VALUES ($1, $2, $3, $4, $5)
    RETURNING id, inventory_entry_id, delta, adjusted_at, reason, created_by, order_line_item_id
), updated AS (
    UPDATE inventory_entries
    SET quantity_on_hand = quantity_on_hand + adj.delta
    FROM adj
    WHERE inventory_entries.id = adj.inventory_entry_id
    RETURNING inventory_entries.quantity_on_hand
)
SELECT adj.id, adj.inventory_entry_id, adj.delta, adj.adjusted_at, adj.reason,
       adj.created_by, adj.order_line_item_id,
       updated.quantity_on_hand AS new_quantity_on_hand
FROM adj
JOIN updated ON TRUE;

-- name: GetOnOrderQtysForParts :many
SELECT dl.part_id,
       COALESCE(SUM(oli.quantity_ordered - oli.quantity_received), 0)::int AS quantity_on_order
FROM order_line_items oli
JOIN distributor_listings dl ON oli.listing_id = dl.id
JOIN orders o ON o.id = oli.order_id
WHERE dl.part_id = ANY(@part_ids::uuid[])
  AND o.team_id = @team_id
  AND o.status IN ('placed', 'partially_received')
  AND oli.quantity_received < oli.quantity_ordered
GROUP BY dl.part_id;

-- name: GetUnallocatedOnOrderForParts :many
-- Returns per-part unallocated inbound: total outstanding on order minus what active builds
-- are still expecting from orders (quantity_needed - quantity_reserved - quantity_received).
SELECT
    dl.part_id,
    GREATEST(0,
        COALESCE(SUM(oli.quantity_ordered - oli.quantity_received), 0)
        - COALESCE((
            SELECT SUM(GREATEST(0, bpa2.quantity_needed - bpa2.quantity_reserved - bpa2.quantity_received))
            FROM build_part_allocations bpa2
            JOIN builds b2 ON bpa2.build_id = b2.id
            WHERE bpa2.part_id = dl.part_id
              AND b2.team_id = @team_id
              AND b2.status NOT IN ('assembled', 'cancelled')
        ), 0)
    )::int AS unallocated_qty
FROM order_line_items oli
JOIN distributor_listings dl ON oli.listing_id = dl.id
JOIN orders o ON o.id = oli.order_id
WHERE dl.part_id = ANY(@part_ids::uuid[])
  AND o.team_id = @team_id
  AND o.status IN ('placed', 'partially_received')
  AND oli.quantity_received < oli.quantity_ordered
GROUP BY dl.part_id;

-- name: GetOrdersWithQtyForPart :many
-- Returns active orders with outstanding unreceived quantity for a specific part.
-- Used at cart conversion to find existing orders whose excess can serve new builds.
SELECT o.id AS order_id,
       COALESCE(SUM(oli.quantity_ordered - oli.quantity_received), 0)::int AS outstanding_qty
FROM order_line_items oli
JOIN distributor_listings dl ON oli.listing_id = dl.id
JOIN orders o ON o.id = oli.order_id
WHERE dl.part_id = @part_id
  AND o.team_id  = @team_id
  AND o.status IN ('placed', 'partially_received')
  AND oli.quantity_received < oli.quantity_ordered
GROUP BY o.id
ORDER BY outstanding_qty DESC;

-- name: GetCommittedQtyForOrderPart :one
-- Returns how much of a given order's quantity for a part is already committed
-- to active builds (ordered/ready) linked to that order.
SELECT COALESCE(SUM(GREATEST(0, bpa.quantity_needed - bpa.quantity_reserved - bpa.quantity_received)), 0)::int AS committed_qty
FROM build_part_allocations bpa
JOIN build_orders bo ON bo.build_id = bpa.build_id
JOIN builds b ON b.id = bpa.build_id
WHERE bo.order_id = @order_id
  AND bpa.part_id = @part_id
  AND b.team_id   = @team_id
  AND b.status    IN ('ordered', 'ready');

-- name: GetOutstandingOrderedQtyForBuildPart :one
SELECT COALESCE(SUM(oli.quantity_ordered - oli.quantity_received), 0)::int AS outstanding_qty
FROM   order_line_items oli
JOIN   distributor_listings dl ON oli.listing_id = dl.id
JOIN   build_orders bo         ON bo.order_id = oli.order_id
WHERE  bo.build_id = $1
  AND  dl.part_id  = $2
  AND  oli.quantity_received < oli.quantity_ordered;

-- name: TransitionOrderToExportedViaAPI :one
UPDATE orders
SET status = 'exported', submission_method = 'api'
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
  AND status IN ('draft', 'exported')
RETURNING id, status, submission_method;

-- name: UpdateOrderIntegrations :one
UPDATE orders
SET integrations = $3
WHERE id = $1 AND team_id = $2
RETURNING id, integrations;

-- name: SetOrderUnresolvedItems :exec
UPDATE orders
SET integrations = jsonb_set(
        COALESCE(integrations, '{}'::jsonb),
        '{digikey,unresolved_items}',
        @unresolved_items,
        true
    )
WHERE id = @id AND team_id = @team_id;

-- name: GetOrderByExternalReference :one
SELECT id FROM orders
WHERE team_id = $1 AND external_reference = $2
LIMIT 1;

-- name: UpdateOrderLineItemExternalData :exec
UPDATE order_line_items
SET unit_price_snapshot       = $2,
    external_unit_price       = $3,
    external_qty_ordered      = $4,
    external_qty_shipped      = $5,
    external_qty_backordered  = $6
WHERE id = $1;

-- name: CreateOrderLineItemForImport :one
INSERT INTO order_line_items (
    order_id, listing_id,
    quantity_ordered, unit_price_snapshot, fixed_order_cost_snapshot,
    external_unit_price, external_qty_ordered, external_qty_shipped, external_qty_backordered
) VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8)
RETURNING *;

-- name: SetOrderLineItemQuantityOrdered :exec
UPDATE order_line_items SET quantity_ordered = @quantity_ordered WHERE id = @id;

-- name: GetOrderLineItemsWithPartID :many
SELECT oli.id, dl.sku, oli.quantity_ordered, oli.quantity_received, dl.part_id
FROM order_line_items oli
JOIN distributor_listings dl ON dl.id = oli.listing_id
WHERE oli.order_id = @order_id;

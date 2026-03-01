-- name: GetDashboardStats :one
SELECT
    (SELECT COUNT(*) FROM projects proj WHERE proj.team_id = $1)::bigint AS project_count,
    (SELECT COUNT(*) FROM projects proj WHERE proj.team_id = $1 AND (proj.status = 'active' OR proj.status = 'completed'))::bigint AS active_project_count,
    (SELECT COUNT(*) FROM inventory_entries inv WHERE inv.team_id = $1)::bigint AS tracked_part_count,
    (SELECT COUNT(*) FROM inventory_entries inv WHERE inv.team_id = $1 AND inv.quantity_on_hand > 0)::bigint AS in_stock_part_count,
    COALESCE((SELECT SUM(inv.quantity_on_hand) FROM inventory_entries inv WHERE inv.team_id = $1), 0)::bigint AS total_quantity_on_hand,
    (SELECT COUNT(*) FROM inventory_entries inv WHERE inv.team_id = $1 AND inv.minimum_stock_quantity IS NOT NULL AND inv.quantity_on_hand < inv.minimum_stock_quantity)::bigint AS inventory_below_restock_count,
    (SELECT COUNT(cli.id) FROM cart_line_items cli JOIN shopping_carts sc ON sc.id = cli.shopping_cart_id WHERE sc.team_id = $1)::bigint AS cart_item_count,
    (SELECT COUNT(*) FROM orders ord WHERE ord.team_id = $1 AND ord.status NOT IN ('received'::order_status, 'cancelled'::order_status))::bigint AS outstanding_order_count;

-- name: GetDashboardRecentProjects :many
SELECT p.id, p.name, p.status,
       br.label AS active_revision_label,
       p.active_bom_revision_id AS active_revision_id
FROM projects p
LEFT JOIN bom_revisions br ON br.id = p.active_bom_revision_id
WHERE p.team_id = $1
ORDER BY p.created_at DESC
LIMIT 5;

-- name: GetDashboardInventoryAlerts :many
SELECT ie.id AS inventory_entry_id, ie.part_id,
       p.mpn, p.description,
       ie.quantity_on_hand, ie.minimum_stock_quantity
FROM inventory_entries ie
JOIN parts p ON p.id = ie.part_id
WHERE ie.team_id = $1
  AND ie.minimum_stock_quantity IS NOT NULL
  AND ie.quantity_on_hand < ie.minimum_stock_quantity
ORDER BY (ie.minimum_stock_quantity - ie.quantity_on_hand) DESC
LIMIT 10;

-- name: GetDashboardOutstandingOrders :many
SELECT o.id, o.status, o.created_at,
       d.name AS distributor_name,
       COUNT(oli.id)::bigint AS item_count
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
LEFT JOIN order_line_items oli ON oli.order_id = o.id
WHERE o.team_id = $1
  AND o.status NOT IN ('received', 'cancelled')
GROUP BY o.id, d.name
ORDER BY o.created_at DESC
LIMIT 5;

-- name: GetDashboardRecentActivity :many
SELECT activity_type, occurred_at, summary FROM (
    SELECT 'build'::text AS activity_type,
           b.built_at AS occurred_at,
           'Built ' || b.boards_built || ' board(s) of ' || pr.name AS summary
    FROM builds b
    JOIN projects pr ON pr.id = b.project_id
    WHERE b.team_id = $1
    UNION ALL
    SELECT 'checkout'::text,
           gc.performed_at,
           COALESCE('Checkout: ' || gc.reason, 'General checkout')
    FROM general_checkouts gc
    WHERE gc.team_id = $1
    UNION ALL
    SELECT 'order_receipt'::text,
           COALESCE(o.received_at, o.created_at),
           'Received order from ' || d.name
    FROM orders o
    JOIN distributors d ON d.id = o.distributor_id
    WHERE o.team_id = $1 AND o.status = 'received'
) activity
ORDER BY occurred_at DESC
LIMIT 10;

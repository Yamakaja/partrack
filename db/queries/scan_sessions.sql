-- name: CreateScanSession :one
INSERT INTO scan_sessions (token_hash, user_id, team_id, context_type, context_order_id, context_order_ref, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, token_hash, user_id, team_id, phone_connected, context_type, context_order_id, context_order_ref, pending_scan, created_at, expires_at;

-- name: GetScanSessionByTokenHash :one
SELECT
    ss.id, ss.token_hash, ss.user_id, ss.team_id,
    ss.phone_connected, ss.context_type, ss.context_order_id, ss.context_order_ref,
    ss.pending_scan, ss.created_at, ss.expires_at,
    u.display_name, u.email, u.is_active, u.is_admin
FROM scan_sessions ss
JOIN users u ON u.id = ss.user_id
WHERE ss.token_hash = $1 AND ss.expires_at > NOW();

-- name: GetScanSession :one
SELECT id, token_hash, user_id, team_id, phone_connected, context_type, context_order_id, context_order_ref, pending_scan, created_at, expires_at
FROM scan_sessions
WHERE id = $1 AND team_id = $2;

-- name: GetActiveScanSessionForUser :one
SELECT id, token_hash, user_id, team_id, phone_connected, context_type, context_order_id, context_order_ref, pending_scan, created_at, expires_at
FROM scan_sessions
WHERE user_id = $1 AND expires_at > NOW()
LIMIT 1;

-- name: SetScanSessionPhoneConnected :one
UPDATE scan_sessions
SET phone_connected = TRUE
WHERE id = $1
RETURNING id, phone_connected, context_type, context_order_id, context_order_ref, pending_scan;

-- name: SetScanSessionPendingScan :one
UPDATE scan_sessions
SET pending_scan = $2
WHERE id = $1
RETURNING id;

-- name: ClearScanSessionPendingScan :one
UPDATE scan_sessions
SET pending_scan = NULL
WHERE id = $1
RETURNING id;

-- name: DeleteScanSession :exec
DELETE FROM scan_sessions WHERE id = $1 AND team_id = $2;

-- name: DeleteExpiredScanSessions :exec
DELETE FROM scan_sessions WHERE expires_at < NOW();

-- name: GetScanSessionByUserID :one
SELECT id, token_hash, user_id, team_id, phone_connected, context_type, context_order_id, context_order_ref, pending_scan, created_at, expires_at
FROM scan_sessions
WHERE id = $1 AND user_id = $2;

-- name: GetOrderLineItemsForScan :many
SELECT
    oli.id,
    p.mpn AS part_mpn,
    p.description AS part_description,
    dl.sku,
    oli.quantity_ordered,
    oli.quantity_received
FROM order_line_items oli
JOIN distributor_listings dl ON dl.id = oli.listing_id
JOIN parts p ON p.id = dl.part_id
WHERE oli.order_id = $1
ORDER BY oli.id;

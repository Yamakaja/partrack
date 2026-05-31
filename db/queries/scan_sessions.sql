-- name: CreateScanSession :one
INSERT INTO scan_sessions (token_hash, user_id, team_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, token_hash, user_id, team_id, phone_connected, pending_scan, created_at, expires_at;

-- name: SetScanSessionTokenHash :one
-- Sets the durable session token hash at pairing time and returns the session's
-- user/team so the phone can be told which context it joined.
UPDATE scan_sessions
SET token_hash = $2
WHERE id = $1
RETURNING id, user_id, team_id, expires_at;

-- name: GetScanSessionByID :one
SELECT id, token_hash, user_id, team_id, phone_connected, pending_scan, created_at, expires_at
FROM scan_sessions
WHERE id = $1 AND expires_at > NOW();

-- name: GetScanSessionByTokenHash :one
SELECT
    ss.id, ss.token_hash, ss.user_id, ss.team_id,
    ss.phone_connected,
    ss.pending_scan, ss.created_at, ss.expires_at,
    u.display_name, u.email, u.is_active, u.is_admin
FROM scan_sessions ss
JOIN users u ON u.id = ss.user_id
WHERE ss.token_hash = $1 AND ss.expires_at > NOW();

-- name: GetScanSession :one
SELECT id, token_hash, user_id, team_id, phone_connected, pending_scan, created_at, expires_at
FROM scan_sessions
WHERE id = $1 AND team_id = $2;

-- name: GetActiveScanSessionForUser :one
SELECT id, token_hash, user_id, team_id, phone_connected, pending_scan, created_at, expires_at
FROM scan_sessions
WHERE user_id = $1 AND expires_at > NOW()
LIMIT 1;

-- name: SetScanSessionPhoneConnected :one
UPDATE scan_sessions
SET phone_connected = TRUE
WHERE id = $1
RETURNING id, phone_connected, pending_scan;

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

-- name: SetScanSessionPhoneDisconnected :exec
UPDATE scan_sessions SET phone_connected = FALSE WHERE id = $1;

-- name: GetScanSessionByUserID :one
SELECT id, token_hash, user_id, team_id, phone_connected, pending_scan, created_at, expires_at
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
JOIN orders o ON o.id = oli.order_id
JOIN distributor_listings dl ON dl.id = oli.listing_id
JOIN parts p ON p.id = dl.part_id
WHERE oli.order_id = $1 AND o.team_id = @team_id
ORDER BY oli.id;

-- name: GetPartBySKUAndDistributorWithInventory :one
-- Looks up a part via distributor_listings.sku scoped to a specific distributor,
-- returning part info and the team's inventory entry (if any).
SELECT
    p.id, p.mpn, p.manufacturer, p.description, p.short_description,
    dl.sku, d.name AS distributor_name,
    ie.id AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label
FROM distributor_listings dl
JOIN parts p ON p.id = dl.part_id
JOIN distributors d ON d.id = dl.distributor_id
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = @team_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE dl.sku = $1
  AND d.name ILIKE @distributor_name
  AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = @team_id))
LIMIT 1;

-- name: GetPartBySKUWithInventory :one
-- Looks up a part via distributor_listings.sku across all distributors,
-- returning part info and the team's inventory entry (if any).
SELECT
    p.id, p.mpn, p.manufacturer, p.description, p.short_description,
    dl.sku, d.name AS distributor_name,
    ie.id AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label
FROM distributor_listings dl
JOIN parts p ON p.id = dl.part_id
JOIN distributors d ON d.id = dl.distributor_id
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = @team_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE dl.sku = $1
  AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = @team_id))
LIMIT 1;

-- name: GetPartByMPNWithInventory :one
-- Looks up a part by MPN, returning part info and the team's inventory entry (if any).
SELECT
    p.id, p.mpn, p.manufacturer, p.description, p.short_description,
    ie.id AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label
FROM parts p
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = @team_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE p.mpn = $1
  AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = @team_id))
ORDER BY p.scope DESC
LIMIT 1;

-- name: GetPartByMPNAndManufacturerWithInventory :one
-- Looks up a part by MPN + manufacturer (case-insensitive), preferring team-scoped.
-- Used for part-only QR codes where manufacturer disambiguates identical MPNs.
SELECT
    p.id, p.mpn, p.manufacturer, p.description, p.short_description,
    ie.id AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label AS storage_location_label
FROM parts p
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = @team_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE p.mpn = $1
  AND p.manufacturer ILIKE $2
  AND (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = @team_id))
ORDER BY p.scope DESC
LIMIT 1;

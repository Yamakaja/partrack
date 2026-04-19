-- name: CreateKittingSession :one
INSERT INTO build_kitting_sessions (scan_session_id, build_id, team_id)
VALUES ($1, $2, $3)
RETURNING id, scan_session_id, build_id, team_id, status, created_at, completed_at;

-- name: SeedKittingConfirmations :exec
INSERT INTO build_kitting_confirmations (kitting_session_id, part_id, quantity_needed)
SELECT @kitting_session_id, bpa.part_id, bpa.quantity_needed
FROM build_part_allocations bpa
WHERE bpa.build_id = @build_id;

-- name: GetKittingSession :one
SELECT
    ks.id, ks.scan_session_id, ks.build_id, ks.team_id, ks.status,
    ks.created_at, ks.completed_at,
    b.quantity AS build_quantity, b.status AS build_status,
    pr.name AS project_name,
    br.label AS bom_revision_label
FROM build_kitting_sessions ks
JOIN builds b       ON b.id = ks.build_id
JOIN projects pr    ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
WHERE ks.id = $1;

-- name: GetActiveKittingSessionForScanSession :one
SELECT id, scan_session_id, build_id, team_id, status, created_at, completed_at
FROM build_kitting_sessions
WHERE scan_session_id = $1 AND status = 'in_progress'
LIMIT 1;

-- name: GetActiveKittingSessionForBuild :one
SELECT id, scan_session_id, build_id, team_id, status, created_at, completed_at
FROM build_kitting_sessions
WHERE build_id = $1 AND status = 'in_progress'
LIMIT 1;

-- name: GetKittingItems :many
-- Returns kitting confirmation rows with part and storage location info.
-- Sorted: pending first, then by storage location path (depth-first), nulls last.
WITH RECURSIVE loc_path AS (
    SELECT sl.id, sl.label, sl.parent_id, sl.label AS path
    FROM storage_locations sl
    WHERE sl.parent_id IS NULL
    UNION ALL
    SELECT sl.id, sl.label, sl.parent_id, lp.path || ' > ' || sl.label
    FROM storage_locations sl
    JOIN loc_path lp ON sl.parent_id = lp.id
)
SELECT
    kc.id,
    kc.kitting_session_id,
    kc.part_id,
    kc.quantity_needed,
    kc.quantity_confirmed,
    kc.status,
    kc.confirmed_at,
    p.mpn        AS part_mpn,
    p.manufacturer AS part_manufacturer,
    p.description AS part_description,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label     AS storage_location_label,
    COALESCE(lp.path, sl.label) AS storage_location_path
FROM build_kitting_confirmations kc
JOIN parts p ON p.id = kc.part_id
LEFT JOIN inventory_entries ie
    ON ie.part_id = kc.part_id AND ie.team_id = @team_id
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
LEFT JOIN loc_path lp ON lp.id = ie.storage_location_id
WHERE kc.kitting_session_id = @kitting_session_id
ORDER BY
    CASE WHEN kc.status = 'pending' THEN 0 ELSE 1 END,
    COALESCE(lp.path, sl.label) NULLS LAST,
    p.mpn NULLS LAST;

-- name: UpsertKittingConfirmation :one
UPDATE build_kitting_confirmations
SET status             = @status,
    quantity_confirmed = @quantity_confirmed,
    confirmed_at       = NOW()
WHERE kitting_session_id = @kitting_session_id AND part_id = @part_id
RETURNING id, kitting_session_id, part_id, quantity_needed, quantity_confirmed, status, confirmed_at;

-- name: GetKittingProgress :many
SELECT status, COUNT(*)::int AS count
FROM build_kitting_confirmations
WHERE kitting_session_id = $1
GROUP BY status;

-- name: CompleteKittingSession :exec
UPDATE build_kitting_sessions
SET status = 'completed', completed_at = NOW()
WHERE id = $1;

-- name: AbandonKittingSession :exec
UPDATE build_kitting_sessions
SET status = 'abandoned'
WHERE id = $1;

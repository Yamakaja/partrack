-- name: CreateActivity :one
-- Single-row insert for the synchronous (critical) lane.
INSERT INTO activity_log (
    team_id, actor_user_id, actor_label, source, action, feed,
    subject_type, subject_id, subject_label, summary, payload, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12
)
RETURNING id;

-- name: InsertActivityRows :copyfrom
-- Bulk insert used by the async batcher (CopyFrom).
INSERT INTO activity_log (
    team_id, actor_user_id, actor_label, source, action, feed,
    subject_type, subject_id, subject_label, summary, payload, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12
);

-- name: ListActivity :many
-- Cursor-paginated feed. scope_team=true → team-wide (feed='team') for purchaser+;
-- scope_team=false → the caller's own rows (any feed). Optional action filter.
SELECT al.id, al.team_id, al.actor_user_id, al.actor_label, al.source,
       al.action, al.feed, al.subject_type, al.subject_id, al.subject_label,
       al.summary, al.payload, al.created_at,
       p.manufacturer      AS part_manufacturer,
       p.image_url         AS part_image_url,
       ie.id               AS inventory_entry_id,
       ie.quantity_on_hand AS inventory_quantity_on_hand
FROM activity_log al
LEFT JOIN parts p              ON al.subject_type = 'part' AND al.subject_id = p.id
LEFT JOIN inventory_entries ie ON al.subject_type = 'part'
                               AND al.subject_id = ie.part_id
                               AND ie.team_id = al.team_id
WHERE al.team_id = @team_id
  AND (
    (sqlc.arg('scope_team')::boolean AND al.feed = 'team')
    OR (NOT sqlc.arg('scope_team')::boolean AND al.actor_user_id = @actor_user_id)
  )
  AND (
    array_length(sqlc.slice('actions')::text[], 1) IS NULL
    OR al.action = ANY(sqlc.slice('actions')::text[])
  )
  AND (
    sqlc.narg('cursor_time')::timestamptz IS NULL
    OR (al.created_at, al.id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY al.created_at DESC, al.id DESC
LIMIT sqlc.arg('limit');

-- name: RecentTeamActivity :many
-- Top-N team feed for the dashboard widget.
SELECT id, actor_label, source, action, feed,
       subject_type, subject_id, subject_label, summary, payload, created_at
FROM activity_log
WHERE team_id = $1 AND feed = 'team'
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: MarkActivityResolved :exec
-- Resolve a 'scan.part_not_found' row by its own id, linking it to the part that
-- was imported off the back of that scan (id carried from the lookup response).
UPDATE activity_log
SET    subject_type  = 'part',
       subject_id    = @part_id,
       subject_label = @part_label
WHERE  id        = @id
  AND  team_id   = @team_id
  AND  action    = 'scan.part_not_found'
  AND  subject_id IS NULL;

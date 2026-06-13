-- name: GetPartNote :one
SELECT team_id, part_id, body, flag, updated_by, updated_at
FROM part_notes
WHERE team_id = $1 AND part_id = $2;

-- name: UpsertPartNote :one
-- Sets the body only; flag is left untouched (defaults to 'none' on first insert).
INSERT INTO part_notes (team_id, part_id, body, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id, part_id) DO UPDATE
  SET body = EXCLUDED.body,
      updated_by = EXCLUDED.updated_by,
      updated_at = NOW()
RETURNING team_id, part_id, body, flag, updated_by, updated_at;

-- name: SetPartFlag :one
-- Sets the team's opinion flag; creates the profile row (empty body) if needed.
INSERT INTO part_notes (team_id, part_id, flag, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id, part_id) DO UPDATE
  SET flag = EXCLUDED.flag,
      updated_by = EXCLUDED.updated_by,
      updated_at = NOW()
RETURNING flag;

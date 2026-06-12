-- name: UpsertBlob :exec
-- Idempotent insert of a content-addressed blob. On hash collision the existing
-- row wins (the bytes are identical, so its metadata is equivalent) — this is the
-- dedup short-circuit at the DB layer.
INSERT INTO blobs (sha256, byte_size, content_type, width, height)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (sha256) DO NOTHING;

-- name: GetBlob :one
SELECT sha256, byte_size, content_type, width, height, thumbnail_sha256, created_at
FROM blobs
WHERE sha256 = $1;

-- name: CreateAttachment :one
INSERT INTO attachments (team_id, blob_sha256, original_filename, uploaded_by)
VALUES ($1, $2, $3, $4)
RETURNING id, team_id, blob_sha256, original_filename, uploaded_by, uploaded_at;

-- name: GetAttachment :one
SELECT id, team_id, blob_sha256, original_filename, uploaded_by, uploaded_at
FROM attachments
WHERE id = $1 AND team_id = $2;

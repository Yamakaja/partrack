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

-- name: GetAttachmentsByIDs :many
-- Batch resolution for the note manifest: maps the attachment UUIDs embedded in
-- a note body to their blob hash + image dimensions, team-scoped so a note can
-- only resolve its own team's attachments.
SELECT a.id, a.blob_sha256, b.content_type, b.width, b.height
FROM attachments a
JOIN blobs b ON b.sha256 = a.blob_sha256
WHERE a.team_id = $1 AND a.id = ANY(sqlc.arg('ids')::uuid[]);

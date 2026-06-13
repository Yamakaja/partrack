-- Document attachments are ordinary `attachments` rows with owner_kind/owner_id
-- set (NULL owner = a note-embedded image, which never appears in these lists).

-- name: CreateDocumentAttachment :one
-- Records a document upload owned by a partrack object. The blob is created
-- separately (UpsertBlob) and shared via content-address dedup.
INSERT INTO attachments (team_id, blob_sha256, original_filename, uploaded_by, owner_kind, owner_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, team_id, blob_sha256, original_filename, uploaded_by, uploaded_at;

-- name: ListDocuments :many
-- Documents attached to one owner object, newest first. Team-scoped so a list
-- only ever exposes the calling team's own attachments. byte_size comes from the
-- shared blob; content type is derived from the filename in the handler.
SELECT a.id, a.blob_sha256, a.original_filename, a.uploaded_by,
       u.display_name AS uploaded_by_display_name,
       a.uploaded_at, b.byte_size
FROM attachments a
JOIN blobs b ON b.sha256 = a.blob_sha256
LEFT JOIN users u ON u.id = a.uploaded_by
WHERE a.team_id = $1 AND a.owner_kind = $2 AND a.owner_id = $3
ORDER BY a.uploaded_at DESC, a.id DESC;

-- name: GetDocument :one
-- Single document for serving/deleting, team-scoped (cross-team IDOR guard).
-- owner_kind IS NOT NULL excludes note-image attachments from the document API.
SELECT a.id, a.blob_sha256, a.original_filename, a.owner_kind, a.owner_id,
       b.byte_size
FROM attachments a
JOIN blobs b ON b.sha256 = a.blob_sha256
WHERE a.id = $1 AND a.team_id = $2 AND a.owner_kind IS NOT NULL;

-- name: DeleteDocument :one
-- Deletes the document attachment row only; the shared blob is retained
-- (no-GC policy). Returns the id so a missing/foreign row is a 404.
DELETE FROM attachments
WHERE id = $1 AND team_id = $2 AND owner_kind IS NOT NULL
RETURNING id;

-- name: ProjectExistsForTeam :one
-- Lightweight ownership probe for document endpoints: returns the id only when
-- the project belongs to the team (→ 404 otherwise, without leaking existence).
SELECT id FROM projects WHERE id = $1 AND team_id = $2;

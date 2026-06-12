-- Content-addressed blob storage. Two layers:
--   blobs       — one row per unique byte sequence (deduplicated by sha256).
--                 Global, NOT team-scoped: identical bytes are one blob no
--                 matter who uploaded them. The bytes themselves live in the
--                 configured blob.Store (local filesystem now, object store
--                 later), addressed by sha256.
--   attachments — one row per upload event. Carries provenance (who/when/
--                 filename) and is the stable opaque handle that note markdown
--                 will reference. team_id scopes access control; the same blob
--                 may back attachments in many teams.
--
-- No GC for now: old/unreferenced blobs are intentionally retained (small, and
-- versioned content is deliberate history). A mark-and-sweep job over
-- attachments(blob_sha256) reachability can be added later.

CREATE TABLE blobs (
  sha256           TEXT        PRIMARY KEY,                -- lowercase hex content address
  byte_size        BIGINT      NOT NULL,
  content_type     TEXT        NOT NULL,                   -- sniffed + validated at upload
  width            INTEGER,                                -- images only; NULL when unknown
  height           INTEGER,
  thumbnail_sha256 TEXT        REFERENCES blobs(sha256),   -- self-ref; NULL until generated
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()      -- first time this content was seen
);

CREATE TABLE attachments (
  id                UUID        PRIMARY KEY DEFAULT uuidv7(),
  team_id           UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  -- RESTRICT (the default): a blob is shared across attachments via dedup, so
  -- deleting an attachment must never cascade-delete the underlying blob.
  blob_sha256       TEXT        NOT NULL REFERENCES blobs(sha256),
  original_filename TEXT,
  uploaded_by       UUID        REFERENCES users(id),       -- NULL if the user is later removed
  uploaded_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX attachments_team ON attachments (team_id);
-- Reachability lookups for a future GC sweep ("which attachments pin this blob").
CREATE INDEX attachments_blob ON attachments (blob_sha256);

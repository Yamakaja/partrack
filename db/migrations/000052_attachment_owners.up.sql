-- Document attachments: an attachment may optionally be "owned" by a partrack
-- object (a part or a project for now) so it shows in that object's document
-- list — separate from images embedded in note markdown, which leave the owner
-- columns NULL and are reachable only via the note body.
--
-- Ownership is an attribute of the upload event, so it lives on `attachments`
-- (one row per upload) rather than in a third table: the blob↔attachment split
-- already exists for dedup, and a document belongs to exactly one object (the
-- same bytes uploaded to two owners dedup to one blob but are two upload rows).
--
-- owner_id is polymorphic, so it carries no FK. Under the existing no-GC policy
-- a deleted owner simply leaves rows that list nowhere; the blob is retained.

ALTER TABLE attachments
  ADD COLUMN owner_kind TEXT,   -- 'part' | 'project' (NULL = free-floating note image)
  ADD COLUMN owner_id   UUID;

-- Partial index: only the "document" attachments participate in owner lookups.
CREATE INDEX attachments_owner ON attachments (team_id, owner_kind, owner_id)
  WHERE owner_kind IS NOT NULL;

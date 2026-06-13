DROP INDEX IF EXISTS attachments_owner;
ALTER TABLE attachments
  DROP COLUMN IF EXISTS owner_id,
  DROP COLUMN IF EXISTS owner_kind;

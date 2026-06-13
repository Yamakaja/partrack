ALTER TABLE projects
  DROP COLUMN IF EXISTS notes,
  DROP COLUMN IF EXISTS notes_updated_by,
  DROP COLUMN IF EXISTS notes_updated_at;

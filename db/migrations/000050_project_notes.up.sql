-- Project notes live on the project row itself: a project is owned by exactly
-- one team (unlike a part, which is global/shared and so needs a per-team
-- part_notes table). Note-specific attribution columns keep "who last edited the
-- note" distinct from other project edits.
ALTER TABLE projects
  ADD COLUMN notes            TEXT        NOT NULL DEFAULT '',
  ADD COLUMN notes_updated_by UUID        REFERENCES users(id),
  ADD COLUMN notes_updated_at TIMESTAMPTZ;

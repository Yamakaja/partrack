-- A team's opinion flag on a part — distinct from the manufacturer lifecycle
-- status. Lives on part_notes (the team's per-part knowledge profile, alongside
-- the note body). `banned` drives a warning when the part is designed into a BOM.
CREATE TYPE part_flag AS ENUM ('none', 'preferred', 'approved', 'banned');

ALTER TABLE part_notes
  ADD COLUMN flag part_flag NOT NULL DEFAULT 'none';

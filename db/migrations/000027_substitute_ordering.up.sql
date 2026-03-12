-- Add ordering to existing substitutes junction table
ALTER TABLE bom_line_item_substitutes
  ADD COLUMN priority INT NOT NULL DEFAULT 0;

-- Add selections_checked_at to builds
ALTER TABLE builds
  ADD COLUMN selections_checked_at TIMESTAMPTZ NULL;

-- Build-level substitute selections (locked at submit time)
CREATE TABLE build_substitute_selections (
  build_id          UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
  bom_line_item_id  UUID NOT NULL REFERENCES bom_line_items(id) ON DELETE CASCADE,
  selected_part_id  UUID NOT NULL REFERENCES parts(id),
  PRIMARY KEY (build_id, bom_line_item_id)
);

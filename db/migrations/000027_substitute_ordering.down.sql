DROP TABLE IF EXISTS build_substitute_selections;
ALTER TABLE builds DROP COLUMN IF EXISTS selections_checked_at;
ALTER TABLE bom_line_item_substitutes DROP COLUMN IF EXISTS priority;

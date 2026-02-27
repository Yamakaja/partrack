ALTER TABLE bom_line_items
    DROP COLUMN IF EXISTS resolution_state,
    DROP COLUMN IF EXISTS ignored;

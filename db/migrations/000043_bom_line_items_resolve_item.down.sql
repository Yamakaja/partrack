DROP INDEX IF EXISTS bom_line_items_resolve_item;
ALTER TABLE bom_line_items
    DROP COLUMN IF EXISTS resolve_item_id;

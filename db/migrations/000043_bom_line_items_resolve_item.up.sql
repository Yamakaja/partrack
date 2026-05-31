-- Link a BOM line item to the part_resolve_item that is importing a new part
-- for it (mirrors bulk_scan_items.resolve_item_id). Nullable: only set for
-- line items whose part was not already known to partrack.
ALTER TABLE bom_line_items
    ADD COLUMN resolve_item_id UUID REFERENCES part_resolve_items(id);

CREATE INDEX bom_line_items_resolve_item
    ON bom_line_items(resolve_item_id)
    WHERE resolve_item_id IS NOT NULL;

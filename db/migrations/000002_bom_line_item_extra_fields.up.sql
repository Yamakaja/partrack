ALTER TABLE bom_line_items
    ADD COLUMN manufacturer     TEXT,
    ADD COLUMN mpn              TEXT,
    ADD COLUMN part_name        TEXT,
    ADD COLUMN sku              TEXT,
    ADD COLUMN distributor_name TEXT;

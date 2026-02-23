ALTER TABLE bom_line_items
    DROP COLUMN IF EXISTS manufacturer,
    DROP COLUMN IF EXISTS mpn,
    DROP COLUMN IF EXISTS part_name,
    DROP COLUMN IF EXISTS sku,
    DROP COLUMN IF EXISTS distributor_name;

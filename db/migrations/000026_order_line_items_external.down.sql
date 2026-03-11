ALTER TABLE order_line_items
  DROP COLUMN IF EXISTS external_unit_price,
  DROP COLUMN IF EXISTS external_qty_ordered,
  DROP COLUMN IF EXISTS external_qty_shipped,
  DROP COLUMN IF EXISTS external_qty_backordered;

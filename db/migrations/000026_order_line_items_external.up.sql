ALTER TABLE order_line_items
  ADD COLUMN external_unit_price      NUMERIC(14,4),
  ADD COLUMN external_qty_ordered     INTEGER,
  ADD COLUMN external_qty_shipped     INTEGER,
  ADD COLUMN external_qty_backordered INTEGER;

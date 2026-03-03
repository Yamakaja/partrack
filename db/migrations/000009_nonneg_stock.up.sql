ALTER TABLE inventory_entries
  ADD CONSTRAINT inventory_qty_nonneg CHECK (quantity_on_hand >= 0);

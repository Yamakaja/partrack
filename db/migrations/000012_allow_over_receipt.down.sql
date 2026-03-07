ALTER TABLE order_line_items ADD CONSTRAINT order_line_items_check CHECK (quantity_received <= quantity_ordered);

ALTER TABLE order_line_items
    ADD CONSTRAINT order_line_items_order_listing_unique UNIQUE (order_id, listing_id);

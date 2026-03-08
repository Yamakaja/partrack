ALTER TABLE cart_line_items
    DROP CONSTRAINT cart_line_items_shopping_cart_id_part_id_key;
ALTER TABLE cart_line_items
    ADD CONSTRAINT cart_line_items_shopping_cart_id_part_id_source_key
    UNIQUE (shopping_cart_id, part_id, source);

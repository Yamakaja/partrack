-- Revert cascade/set-null changes back to default RESTRICT behaviour.

ALTER TABLE bulk_scan_items
    DROP CONSTRAINT bulk_scan_items_resolve_item_id_fkey,
    ADD CONSTRAINT bulk_scan_items_resolve_item_id_fkey
        FOREIGN KEY (resolve_item_id) REFERENCES part_resolve_items(id);

ALTER TABLE cart_line_items
    DROP CONSTRAINT cart_line_items_source_bom_line_item_id_fkey,
    ADD CONSTRAINT cart_line_items_source_bom_line_item_id_fkey
        FOREIGN KEY (source_bom_line_item_id) REFERENCES bom_line_items(id);

ALTER TABLE bom_line_item_substitutes
    DROP CONSTRAINT bom_line_item_substitutes_bom_line_item_id_fkey,
    ADD CONSTRAINT bom_line_item_substitutes_bom_line_item_id_fkey
        FOREIGN KEY (bom_line_item_id) REFERENCES bom_line_items(id);

ALTER TABLE stock_adjustments
    DROP CONSTRAINT sa_order_line_item_fk,
    ADD CONSTRAINT sa_order_line_item_fk
        FOREIGN KEY (order_line_item_id) REFERENCES order_line_items(id);

ALTER TABLE cart_line_items
    DROP CONSTRAINT cart_line_items_listing_id_fkey,
    ADD CONSTRAINT cart_line_items_listing_id_fkey
        FOREIGN KEY (listing_id) REFERENCES distributor_listings(id);

ALTER TABLE order_line_items
    DROP CONSTRAINT order_line_items_listing_id_fkey,
    ADD CONSTRAINT order_line_items_listing_id_fkey
        FOREIGN KEY (listing_id) REFERENCES distributor_listings(id);

ALTER TABLE inventory_entries
    DROP CONSTRAINT inventory_entries_preferred_listing_id_fkey,
    ADD CONSTRAINT inventory_entries_preferred_listing_id_fkey
        FOREIGN KEY (preferred_listing_id) REFERENCES distributor_listings(id);

ALTER TABLE stock_adjustments
    DROP CONSTRAINT stock_adjustments_inventory_entry_id_fkey,
    ADD CONSTRAINT stock_adjustments_inventory_entry_id_fkey
        FOREIGN KEY (inventory_entry_id) REFERENCES inventory_entries(id);

ALTER TABLE build_substitute_selections
    DROP CONSTRAINT build_substitute_selections_selected_part_id_fkey,
    ADD CONSTRAINT build_substitute_selections_selected_part_id_fkey
        FOREIGN KEY (selected_part_id) REFERENCES parts(id);

ALTER TABLE build_kitting_confirmations
    DROP CONSTRAINT build_kitting_confirmations_part_id_fkey,
    ADD CONSTRAINT build_kitting_confirmations_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE part_resolve_items
    DROP CONSTRAINT part_resolve_items_resolved_part_id_fkey,
    ADD CONSTRAINT part_resolve_items_resolved_part_id_fkey
        FOREIGN KEY (resolved_part_id) REFERENCES parts(id);

ALTER TABLE bulk_scan_items
    DROP CONSTRAINT bulk_scan_items_part_id_fkey,
    ADD CONSTRAINT bulk_scan_items_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE general_checkout_line_items
    DROP CONSTRAINT general_checkout_line_items_part_id_fkey,
    ADD CONSTRAINT general_checkout_line_items_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE build_part_allocations
    DROP CONSTRAINT build_part_allocations_part_id_fkey,
    ADD CONSTRAINT build_part_allocations_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE cart_line_items
    DROP CONSTRAINT cart_line_items_part_id_fkey,
    ADD CONSTRAINT cart_line_items_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE bom_line_item_substitutes
    DROP CONSTRAINT bom_line_item_substitutes_part_id_fkey,
    ADD CONSTRAINT bom_line_item_substitutes_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE bom_line_items
    DROP CONSTRAINT bom_line_items_resolved_part_id_fkey,
    ADD CONSTRAINT bom_line_items_resolved_part_id_fkey
        FOREIGN KEY (resolved_part_id) REFERENCES parts(id);

ALTER TABLE inventory_entries
    DROP CONSTRAINT inventory_entries_part_id_fkey,
    ADD CONSTRAINT inventory_entries_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE distributor_listings
    DROP CONSTRAINT distributor_listings_part_id_fkey,
    ADD CONSTRAINT distributor_listings_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

ALTER TABLE part_parameters
    DROP CONSTRAINT part_parameters_part_id_fkey,
    ADD CONSTRAINT part_parameters_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

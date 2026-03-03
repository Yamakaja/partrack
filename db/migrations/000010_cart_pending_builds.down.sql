DROP TABLE cart_pending_builds;

ALTER TABLE cart_line_items
    ADD CONSTRAINT bom_shortfall_requires_line_item CHECK (
        source != 'bom_shortfall' OR source_bom_line_item_id IS NOT NULL
    );

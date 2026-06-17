ALTER TABLE bom_line_items DROP CONSTRAINT bom_line_items_interpretation_source_check;
ALTER TABLE bom_line_items ADD CONSTRAINT bom_line_items_interpretation_source_check
    CHECK (interpretation_source IN ('auto', 'user'));

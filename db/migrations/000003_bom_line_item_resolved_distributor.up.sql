ALTER TABLE bom_line_items
    ADD COLUMN resolved_distributor_id UUID REFERENCES distributors(id);

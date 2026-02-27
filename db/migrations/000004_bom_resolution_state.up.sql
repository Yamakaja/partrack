ALTER TABLE bom_line_items
    ADD COLUMN resolution_state TEXT,   -- 'auto_sku'|'auto_mpn'|'ambiguous'|'manual'|'ignored'|NULL
    ADD COLUMN ignored BOOLEAN NOT NULL DEFAULT FALSE;

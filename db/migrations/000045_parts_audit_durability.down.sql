-- Restore the cascade FKs from migration 000042.
ALTER TABLE inventory_entries
    DROP CONSTRAINT inventory_entries_part_id_fkey,
    ADD CONSTRAINT inventory_entries_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id) ON DELETE CASCADE;

ALTER TABLE stock_adjustments
    DROP CONSTRAINT stock_adjustments_inventory_entry_id_fkey,
    ADD CONSTRAINT stock_adjustments_inventory_entry_id_fkey
        FOREIGN KEY (inventory_entry_id) REFERENCES inventory_entries(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS parts_not_deleted;
ALTER TABLE parts DROP COLUMN deleted_at;
ALTER TABLE parts DROP COLUMN created_by;

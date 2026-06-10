-- Make parts an audit-durable source-of-truth for the activity_log Complement
-- model: record who created a part, and soft-delete instead of hard-deleting so
-- the creation record (and the stock ledger hanging off it) is never erased.
ALTER TABLE parts ADD COLUMN created_by UUID REFERENCES users(id);
ALTER TABLE parts ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX parts_not_deleted ON parts (id) WHERE deleted_at IS NULL;

-- Revert the audit-critical cascades introduced in 000042. With soft-delete the
-- cascade can never fire, but leaving ON DELETE CASCADE on the stock ledger is a
-- footgun: a stray delete would erase immutable audit history. Restore RESTRICT.
ALTER TABLE stock_adjustments
    DROP CONSTRAINT stock_adjustments_inventory_entry_id_fkey,
    ADD CONSTRAINT stock_adjustments_inventory_entry_id_fkey
        FOREIGN KEY (inventory_entry_id) REFERENCES inventory_entries(id);

ALTER TABLE inventory_entries
    DROP CONSTRAINT inventory_entries_part_id_fkey,
    ADD CONSTRAINT inventory_entries_part_id_fkey
        FOREIGN KEY (part_id) REFERENCES parts(id);

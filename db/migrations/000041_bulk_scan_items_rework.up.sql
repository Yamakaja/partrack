-- Migrate existing active items to the new pending_review status.
UPDATE bulk_scan_items
SET status = 'pending_review'
WHERE status NOT IN ('reviewed', 'dismissed');

-- Rename 'unrecognized_barcode' task_type to match part_resolve_items.
UPDATE bulk_scan_items
SET task_type = 'unrecognized'
WHERE task_type = 'unrecognized_barcode';

-- Remove enrichment columns (now live on part_resolve_items).
ALTER TABLE bulk_scan_items
    DROP COLUMN IF EXISTS enrichment_data,
    DROP COLUMN IF EXISTS enrichment_job_id,
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS enriched_at;

-- Add FK to the resolve item (nullable).
ALTER TABLE bulk_scan_items
    ADD COLUMN resolve_item_id UUID REFERENCES part_resolve_items(id);

-- Change the default status value.
ALTER TABLE bulk_scan_items
    ALTER COLUMN status SET DEFAULT 'pending_review';

-- Replace status CHECK with new three-value enum.
ALTER TABLE bulk_scan_items
    DROP CONSTRAINT IF EXISTS bulk_scan_items_status_check;
ALTER TABLE bulk_scan_items
    ADD CONSTRAINT bulk_scan_items_status_check
    CHECK (status IN ('pending_review', 'reviewed', 'dismissed'));

-- Update task_type CHECK to use 'unrecognized' (no longer 'unrecognized_barcode').
ALTER TABLE bulk_scan_items
    DROP CONSTRAINT IF EXISTS bulk_scan_items_task_type_check;
ALTER TABLE bulk_scan_items
    ADD CONSTRAINT bulk_scan_items_task_type_check
    CHECK (task_type IS NULL OR task_type IN (
        'unknown_part_import',
        'known_part_create_entry',
        'known_part_stock_adjustment',
        'unrecognized'
    ));

-- Drop and recreate the partial unique index with the new status vocabulary.
DROP INDEX IF EXISTS bulk_scan_items_active_barcode_idx;
CREATE UNIQUE INDEX bulk_scan_items_active_barcode_idx
    ON bulk_scan_items (session_id, barcode)
    WHERE status NOT IN ('dismissed', 'reviewed');

CREATE INDEX bulk_scan_items_resolve_item
    ON bulk_scan_items(resolve_item_id)
    WHERE resolve_item_id IS NOT NULL;

DROP INDEX IF EXISTS bulk_scan_items_resolve_item;
DROP INDEX IF EXISTS bulk_scan_items_active_barcode_idx;

ALTER TABLE bulk_scan_items
    DROP COLUMN IF EXISTS resolve_item_id;

ALTER TABLE bulk_scan_items
    ADD COLUMN enrichment_data  JSONB,
    ADD COLUMN enrichment_job_id UUID,
    ADD COLUMN error_message    TEXT,
    ADD COLUMN enriched_at      TIMESTAMPTZ;

ALTER TABLE bulk_scan_items
    ALTER COLUMN status SET DEFAULT 'pending_enrichment';

ALTER TABLE bulk_scan_items
    DROP CONSTRAINT IF EXISTS bulk_scan_items_status_check;
ALTER TABLE bulk_scan_items
    ADD CONSTRAINT bulk_scan_items_status_check
    CHECK (status IN ('pending_enrichment', 'enriched', 'reviewed', 'dismissed', 'error'));

ALTER TABLE bulk_scan_items
    DROP CONSTRAINT IF EXISTS bulk_scan_items_task_type_check;
ALTER TABLE bulk_scan_items
    ADD CONSTRAINT bulk_scan_items_task_type_check
    CHECK (task_type IS NULL OR task_type IN (
        'unknown_part_import',
        'known_part_create_entry',
        'known_part_stock_adjustment',
        'unrecognized_barcode'
    ));

CREATE UNIQUE INDEX bulk_scan_items_active_barcode_idx
    ON bulk_scan_items (session_id, barcode)
    WHERE status NOT IN ('dismissed', 'reviewed');

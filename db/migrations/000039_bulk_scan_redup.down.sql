DROP INDEX IF EXISTS bulk_scan_items_active_barcode_idx;

ALTER TABLE bulk_scan_items ADD CONSTRAINT bulk_scan_items_session_id_barcode_key UNIQUE (session_id, barcode);

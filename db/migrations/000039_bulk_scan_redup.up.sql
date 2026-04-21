-- Replace the full unique constraint with a partial unique index so that a
-- barcode can be re-scanned after its previous review task has been dismissed
-- or reviewed. Only one *active* (pending / enriched / error) task per barcode
-- per session is still prevented.
ALTER TABLE bulk_scan_items DROP CONSTRAINT bulk_scan_items_session_id_barcode_key;

CREATE UNIQUE INDEX bulk_scan_items_active_barcode_idx
  ON bulk_scan_items (session_id, barcode)
  WHERE status NOT IN ('dismissed', 'reviewed');

CREATE TABLE bulk_scan_sessions (
  id           UUID        PRIMARY KEY DEFAULT uuidv7(),
  user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id      UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  closed_at    TIMESTAMPTZ,
  last_item_at TIMESTAMPTZ,
  deleted_at   TIMESTAMPTZ
);

CREATE INDEX bulk_scan_sessions_user ON bulk_scan_sessions(user_id, closed_at, deleted_at);

CREATE TABLE bulk_scan_items (
  id                UUID        PRIMARY KEY DEFAULT uuidv7(),
  session_id        UUID        NOT NULL REFERENCES bulk_scan_sessions(id) ON DELETE CASCADE,
  barcode           TEXT        NOT NULL,
  status            TEXT        NOT NULL DEFAULT 'pending_enrichment'
                                CHECK (status IN ('pending_enrichment', 'enriched', 'reviewed', 'dismissed', 'error')),
  task_type         TEXT
                                CHECK (task_type IS NULL OR task_type IN (
                                  'unknown_part_import',
                                  'known_part_create_entry',
                                  'known_part_stock_adjustment',
                                  'unrecognized_barcode'
                                )),
  parsed_fields     JSONB,
  initial_qty       INT,
  part_id           UUID        REFERENCES parts(id),
  enrichment_data   JSONB,
  enrichment_job_id UUID,
  error_message     TEXT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  enriched_at       TIMESTAMPTZ,
  UNIQUE (session_id, barcode)
);

CREATE INDEX bulk_scan_items_session_status ON bulk_scan_items(session_id, status);

CREATE TABLE scan_audit_log (
  id                    UUID        PRIMARY KEY DEFAULT uuidv7(),
  team_id               UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id               UUID        NOT NULL REFERENCES users(id),
  session_id            UUID        REFERENCES scan_sessions(id) ON DELETE SET NULL,
  event_type            TEXT        NOT NULL,
  entity_type           TEXT,
  entity_id             UUID,
  entity_name           TEXT,
  quantity              INT,
  parsed_sku            TEXT,
  parsed_distributor_id TEXT,
  parsed_mpn            TEXT,
  parsed_manufacturer   TEXT,
  metadata              JSONB,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX scan_audit_log_user_time
  ON scan_audit_log(team_id, user_id, created_at DESC);

CREATE INDEX scan_audit_log_sku_resolution
  ON scan_audit_log(parsed_sku, parsed_distributor_id)
  WHERE event_type = 'part_not_found' AND entity_id IS NULL;

CREATE INDEX scan_audit_log_mpn_resolution
  ON scan_audit_log(parsed_mpn, parsed_manufacturer)
  WHERE event_type = 'part_not_found' AND entity_id IS NULL;

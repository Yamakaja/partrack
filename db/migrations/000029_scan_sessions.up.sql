CREATE TABLE scan_sessions (
  id               UUID        PRIMARY KEY DEFAULT uuidv7(),
  token_hash       TEXT        NOT NULL UNIQUE,
  user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id          UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  phone_connected  BOOLEAN     NOT NULL DEFAULT FALSE,
  context_type     TEXT,
  context_order_id UUID,
  context_order_ref TEXT,
  pending_scan     JSONB,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at       TIMESTAMPTZ NOT NULL
);

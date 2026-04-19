CREATE TABLE build_kitting_sessions (
  id              UUID        PRIMARY KEY DEFAULT uuidv7(),
  scan_session_id UUID        NOT NULL REFERENCES scan_sessions(id) ON DELETE CASCADE,
  build_id        UUID        NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
  team_id         UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  status          TEXT        NOT NULL DEFAULT 'in_progress'
                              CHECK (status IN ('in_progress', 'completed', 'abandoned')),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at    TIMESTAMPTZ
);

CREATE INDEX build_kitting_sessions_scan  ON build_kitting_sessions(scan_session_id);
CREATE INDEX build_kitting_sessions_build ON build_kitting_sessions(build_id);

CREATE TABLE build_kitting_confirmations (
  id                 UUID        PRIMARY KEY DEFAULT uuidv7(),
  kitting_session_id UUID        NOT NULL REFERENCES build_kitting_sessions(id) ON DELETE CASCADE,
  part_id            UUID        NOT NULL REFERENCES parts(id),
  quantity_needed    INT         NOT NULL CHECK (quantity_needed > 0),
  quantity_confirmed INT         NOT NULL DEFAULT 0,
  status             TEXT        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending', 'confirmed', 'shortfall', 'skipped')),
  confirmed_at       TIMESTAMPTZ,
  UNIQUE (kitting_session_id, part_id)
);

CREATE INDEX build_kitting_confirmations_session ON build_kitting_confirmations(kitting_session_id);

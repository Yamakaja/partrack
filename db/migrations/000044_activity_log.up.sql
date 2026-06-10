-- Generalize the scanner-specific scan_audit_log into a unified activity_log.
-- Records are a *complement* to domain ledgers (e.g. stock_adjustments): a row
-- exists only when no domain source-of-truth already captures the change with
-- actor + timestamp. Rows are self-contained for display (snapshot labels) and
-- carry a structured payload the frontend renders via a declarative template.
--
-- No production installations exist, so scan_audit_log is dropped outright.
DROP TABLE IF EXISTS scan_audit_log;

CREATE TABLE activity_log (
  id            UUID        PRIMARY KEY DEFAULT uuidv7(),
  team_id       UUID        REFERENCES teams(id) ON DELETE CASCADE,   -- NULL: global/admin scope
  actor_user_id UUID        REFERENCES users(id),                     -- NULL: job/system actor
  actor_label   TEXT,                                                 -- snapshot of actor display name
  source        TEXT        NOT NULL,        -- web | scanner | job | system
  action        TEXT        NOT NULL,        -- verb.noun, e.g. 'part.created'
  feed          TEXT        NOT NULL,        -- team | personal | audit
  subject_type  TEXT,                        -- primary entity type, e.g. 'part'
  subject_id    UUID,
  subject_label TEXT,                        -- snapshot for display + durability
  summary       TEXT,                        -- plain-text fallback / audit export
  payload       JSONB,                       -- { refs:{}, values:{}, diff:{} }
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Team-wide feed (purchaser+ toggle): the high-signal operational stream.
CREATE INDEX activity_team_feed_time
  ON activity_log (team_id, created_at DESC, id DESC)
  WHERE feed = 'team';

-- Per-user "my activity" panel (default scope).
CREATE INDEX activity_actor_time
  ON activity_log (team_id, actor_user_id, created_at DESC, id DESC);

-- Future "history of this entity" view.
CREATE INDEX activity_subject
  ON activity_log (subject_type, subject_id, created_at DESC)
  WHERE subject_id IS NOT NULL;

-- Resolution of a 'scan.part_not_found' row when the part is later imported
-- (keyed on id, carried explicitly from the lookup response — no fuzzy matching).
CREATE INDEX activity_part_not_found_unresolved
  ON activity_log (id)
  WHERE action = 'scan.part_not_found' AND subject_id IS NULL;

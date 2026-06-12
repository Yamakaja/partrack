-- Per-team freeform markdown notes on a part — the team's *knowledge* profile
-- for a part, parallel to the inventory entry (its *stock* profile). One note
-- per (team, part); created lazily on first save. The body is markdown that may
-- embed `attachment:<uuid>` image refs and `:part[label]{#uuid}` object links,
-- resolved to a manifest at read time.
CREATE TABLE part_notes (
  team_id    UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  part_id    UUID        NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  body       TEXT        NOT NULL DEFAULT '',
  updated_by UUID        REFERENCES users(id),         -- NULL if the user is later removed
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (team_id, part_id)
);

-- Team-level interpretation memory (see doc/bom-import.md §9.2).
--
-- A team-scoped cache of confirmed genspec interpretations, keyed by the
-- normalised (class, value, footprint) of a BOM line. It is written whenever a
-- user corrects an interpretation in the import review gate, and consulted on
-- every subsequent import so the same correction is reused across revisions AND
-- across projects — the system gets less wrong over time. This is to per-line
-- interpretation what bom_import_config is to a one-off column map.
CREATE TABLE bom_interpretation_memory (
    id             UUID        PRIMARY KEY DEFAULT uuidv7(),
    team_id        UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    class          TEXT        NOT NULL,
    value_key      TEXT        NOT NULL,
    footprint_key  TEXT        NOT NULL,
    interpretation JSONB       NOT NULL,
    created_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, class, value_key, footprint_key)
);

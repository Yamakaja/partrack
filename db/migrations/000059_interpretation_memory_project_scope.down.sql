-- Revert to the team-scoped interpretation memory (see 000055). Rebuildable
-- cache, so dropping the project-scoped rows on the way down is safe.
DROP TABLE bom_interpretation_memory;

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

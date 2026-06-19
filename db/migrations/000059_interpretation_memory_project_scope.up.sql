-- Rescope interpretation memory from team to project (Option A).
--
-- The team-wide key let a correction made for one project silently change how a
-- DIFFERENT project's BOM was interpreted — including leaking LLM-derived
-- identity fields (mpn/sku) across unrelated designs. Interpretation memory is a
-- rebuildable cache (a missing entry just falls back to a fresh parse), so it is
-- safe to discard existing rows here; they repopulate as users review imports.
DROP TABLE bom_interpretation_memory;

CREATE TABLE bom_interpretation_memory (
    id             UUID        PRIMARY KEY DEFAULT uuidv7(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    class          TEXT        NOT NULL,
    value_key      TEXT        NOT NULL,
    footprint_key  TEXT        NOT NULL,
    interpretation JSONB       NOT NULL,
    created_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, class, value_key, footprint_key)
);

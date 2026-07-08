-- Distributor sourcing policy: ranked distributor preference + out-of-stock fallback.

-- Per-team ordered preference over distributors. Lower value = higher preference.
-- NULL = unranked (no explicit preference). Backfilled from the legacy `preferred` flag.
ALTER TABLE distributor_configs
    ADD COLUMN priority INT;

UPDATE distributor_configs
SET priority = 1
WHERE preferred = TRUE;

-- Team-level sourcing strategy (owned by the purchaser). One row per team; absent
-- row is treated as the default `cost` strategy, so existing teams are unaffected.
CREATE TABLE team_sourcing_settings (
    team_id            UUID        PRIMARY KEY REFERENCES teams(id) ON DELETE CASCADE,
    strategy           TEXT        NOT NULL DEFAULT 'cost'
                       CHECK (strategy IN ('cost', 'preferred_in_stock')),
    -- Optional guardrail: only honor the preferred distributor when its total is within
    -- pct / abs of the cheapest option. NULL on both = honor preference at any price.
    cost_tolerance_pct NUMERIC(6,2),
    cost_tolerance_abs NUMERIC(14,4),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

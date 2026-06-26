-- Canonical manufacturers and their name aliases.
--
-- Distributors spell the same manufacturer differently ("Texas Instruments" at
-- DigiKey, "TI" at another). These tables let the resolver (internal/mfrmatch)
-- collapse those spellings to one canonical identity, so cross-distributor part
-- discovery (enrichByMPN) and part dedup (ApplyEnrichmentResult) treat them as the
-- same manufacturer instead of dropping listings or creating duplicate parts.
--
-- The alias table is the only equivalence store; matching always falls back to
-- conservative string normalization when a spelling is unknown, so an empty table
-- reproduces the previous behaviour exactly.

-- created_by is a bare UUID, NOT a FK to users(id), on purpose. These are seeded
-- reference tables that must survive the test harness's `TRUNCATE users ... CASCADE`
-- (a FK to users would cascade and wipe the seed) — the same reason categories and
-- parameter_definitions carry no FK to truncated tables. It also keeps the audit
-- trail intact if a user is later deleted.
CREATE TABLE manufacturers (
    id              UUID        PRIMARY KEY DEFAULT uuidv7(),
    canonical_name  TEXT        NOT NULL,                 -- preferred display spelling
    normalized_name TEXT        NOT NULL UNIQUE,          -- mfrmatch.Normalize(canonical_name); the match key
    created_by      UUID,                                 -- admin who created it; NULL for seed
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE manufacturer_aliases (
    id               UUID        PRIMARY KEY DEFAULT uuidv7(),
    manufacturer_id  UUID        NOT NULL REFERENCES manufacturers(id) ON DELETE CASCADE,
    alias            TEXT        NOT NULL,                -- as-seen spelling (display / provenance)
    alias_normalized TEXT        NOT NULL UNIQUE,         -- mfrmatch.Normalize(alias); one canonical per spelling
    source           TEXT        NOT NULL DEFAULT 'manual'
                        CHECK (source IN ('seed', 'learned', 'manual')),
    distributor_id   UUID        REFERENCES distributors(id) ON DELETE SET NULL, -- which distributor spelled it this way
    created_by       UUID,                               -- admin who created it; NULL for seed
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_manufacturer_aliases_mfr ON manufacturer_aliases (manufacturer_id);

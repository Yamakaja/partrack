-- Bookkeeping for one-time data backfills that require application (Go) logic and
-- therefore cannot be expressed as plain SQL migrations — e.g. normalising
-- parts.canonical_package from the raw parts.package via genspec.NormalizePackage,
-- a token scanner that would be fragile to reproduce in SQL. Each backfill writes
-- a marker row here on success so it runs exactly once per database. See
-- internal/db/backfill.go.
CREATE TABLE app_backfills (
    name       TEXT        PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

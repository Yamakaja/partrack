-- Irreversible by design: the up migration fills previously-empty display
-- labels, and which rows were empty is not recorded anywhere. Clearing labels
-- again would also wipe the snapshots newer rows legitimately carry, so the
-- down migration is intentionally a no-op.
SELECT 1;

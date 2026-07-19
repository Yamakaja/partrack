-- Provenance for inherited resolutions.
--
-- A line resolved by the prior-revision tier records 'auto_prior_revision', which on
-- a maturing project becomes the overwhelming majority of rows (38 of 46 in the
-- reference project) and is a pointer to a pointer: the revision it points at had
-- usually inherited the decision too. These columns collapse the chain at write time
-- so the revision page can name the decision that actually determined the part
-- instead of repeating "matched to last revision" on every row.
--
-- NULL on a non-inherited row — there the row's own resolution_state IS the origin.
ALTER TABLE bom_line_items
    ADD COLUMN origin_resolution_state TEXT,
    ADD COLUMN origin_revision_id      UUID REFERENCES bom_revisions(id) ON DELETE SET NULL;

-- Backfill existing inherited rows. Keying on the resolved part rather than on the
-- reuse key (MPN+manufacturer / MPN / SKU+distributor / line signature) is
-- tier-agnostic: whichever of the four tiers fired, the outcome was this part, so the
-- most recent earlier revision that reached the same part under a decision of its own
-- is the origin. Verified against the reference project — identical results to
-- matching on reference_designators across all inherited rows.
WITH inherited AS (
    SELECT b.id, b.resolved_part_id, br.project_id, br.imported_at
    FROM bom_line_items b
    JOIN bom_revisions br ON br.id = b.bom_revision_id
    WHERE b.resolution_state = 'auto_prior_revision'
      AND b.resolved_part_id IS NOT NULL
),
origins AS (
    SELECT i.id, o.state, o.rev_id
    FROM inherited i
    CROSS JOIN LATERAL (
        SELECT b2.resolution_state AS state, br2.id AS rev_id
        FROM bom_line_items b2
        JOIN bom_revisions br2 ON br2.id = b2.bom_revision_id
        WHERE br2.project_id      = i.project_id
          AND br2.imported_at     < i.imported_at
          AND b2.resolved_part_id = i.resolved_part_id
          AND b2.resolution_state IS NOT NULL
          AND b2.resolution_state <> 'auto_prior_revision'
        ORDER BY br2.imported_at DESC
        LIMIT 1
    ) o
)
UPDATE bom_line_items b
SET origin_resolution_state = origins.state,
    origin_revision_id      = origins.rev_id
FROM origins
WHERE origins.id = b.id;

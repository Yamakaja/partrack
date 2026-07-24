-- Backfill activity_log.subject_label for rows written before the record sites
-- started snapshotting it. Without a label the feed renders the generic
-- "the order" / "the build" fallback, so existing history reads as anonymous.
--
-- These reconstruct the label from the entity's CURRENT state, not its state at
-- event time — a true snapshot is unrecoverable for past rows. New rows carry a
-- real snapshot (see internal/activity/label.go, which these mirror).
--
-- Only empty labels are filled; existing snapshots are never rewritten.

-- Orders: distributor + the most specific reference, else the short id.
UPDATE activity_log a
SET    subject_label = d.name || COALESCE(
           ' #' || NULLIF(LTRIM(COALESCE(NULLIF(BTRIM(o.purchase_order_number), ''),
                                         NULLIF(BTRIM(o.external_reference), '')), '#'), ''),
           ' ' || SPLIT_PART(o.id::text, '-', 1))
FROM   orders o
JOIN   distributors d ON d.id = o.distributor_id
WHERE  a.subject_type = 'order'
  AND  a.subject_id = o.id
  AND  COALESCE(a.subject_label, '') = '';

-- Builds: project + BOM revision, not doubling a "rev"/"v" already on the label.
UPDATE activity_log a
SET    subject_label = BTRIM(pr.name || ' ' || CASE
           WHEN COALESCE(BTRIM(br.label), '') = '' THEN ''
           WHEN LOWER(BTRIM(br.label)) LIKE 'rev%'
             OR LOWER(BTRIM(br.label)) LIKE 'v%'   THEN BTRIM(br.label)
           ELSE 'rev ' || BTRIM(br.label)
       END)
FROM   builds b
JOIN   projects pr ON pr.id = b.project_id
JOIN   bom_revisions br ON br.id = b.bom_revision_id
WHERE  a.subject_type = 'build'
  AND  a.subject_id = b.id
  AND  COALESCE(a.subject_label, '') = '';

-- Parts: MPN, falling back to the short description for generic parts.
-- Unresolved scan.part_not_found rows have a NULL subject_id and are untouched.
UPDATE activity_log a
SET    subject_label = COALESCE(NULLIF(BTRIM(p.mpn), ''), NULLIF(BTRIM(p.short_description), ''))
FROM   parts p
WHERE  a.subject_type = 'part'
  AND  a.subject_id = p.id
  AND  COALESCE(a.subject_label, '') = ''
  AND  COALESCE(NULLIF(BTRIM(p.mpn), ''), NULLIF(BTRIM(p.short_description), '')) IS NOT NULL;

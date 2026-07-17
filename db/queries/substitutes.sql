-- name: ListSubstitutesForRevisionResolved :many
-- All approved substitutes of a revision's resolved line items, with the
-- primary line's identity — the scanner filter's "substitute" verdict set.
SELECT s.bom_line_item_id, s.part_id,
       p.mpn, COALESCE(p.short_description, p.description, '') AS description,
       bli.reference_designators, bli.resolved_part_id
FROM bom_line_item_substitutes s
JOIN bom_line_items bli ON bli.id = s.bom_line_item_id
JOIN parts p ON p.id = s.part_id
WHERE bli.bom_revision_id = $1 AND bli.resolved_part_id IS NOT NULL;

-- name: ListSubstitutesForLineItem :many
-- Returns substitutes ordered by priority (0 = highest)
SELECT s.part_id, p.mpn AS part_mpn, s.priority,
       p.manufacturer,
       COALESCE(p.short_description, p.description) AS description,
       p.lifecycle_status,
       ie.quantity_on_hand
FROM bom_line_item_substitutes s
JOIN parts p ON p.id = s.part_id
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = $2
WHERE s.bom_line_item_id = $1
ORDER BY s.priority ASC;

-- name: AddSubstitute :one
-- Appends a substitute at the end of the priority list (MAX + 1)
INSERT INTO bom_line_item_substitutes (bom_line_item_id, part_id, priority)
VALUES (
  $1, $2,
  COALESCE((SELECT MAX(priority) FROM bom_line_item_substitutes WHERE bom_line_item_id = $1), -1) + 1
)
ON CONFLICT (bom_line_item_id, part_id) DO NOTHING
RETURNING part_id, priority;

-- name: RemoveSubstitute :exec
DELETE FROM bom_line_item_substitutes
WHERE bom_line_item_id = $1 AND part_id = $2;

-- name: ReorderSubstitutes :exec
-- Bulk-update priorities from an ordered list; row_number gives 1-based index → store 0-based
UPDATE bom_line_item_substitutes AS s
SET priority = ord.rn - 1
FROM (
  SELECT unnest($2::uuid[]) AS part_id, row_number() OVER () AS rn
) AS ord
WHERE s.bom_line_item_id = $1 AND s.part_id = ord.part_id;

-- name: ImportSubstitutesFromRevision :execrows
-- Copy substitutes from source revision's line item (matched by resolved_part_id)
-- into the target line item, appending after existing priorities. Skips duplicates.
WITH source_line_item AS (
  SELECT bli2.id FROM bom_line_items bli2
  WHERE bli2.bom_revision_id = $2
    AND bli2.resolved_part_id = (
      SELECT bli1.resolved_part_id FROM bom_line_items bli1 WHERE bli1.id = $1
    )
  LIMIT 1
),
max_existing AS (
  SELECT COALESCE(MAX(priority), -1) AS max_p
  FROM bom_line_item_substitutes WHERE bom_line_item_id = $1
),
source_subs AS (
  SELECT s.part_id,
         ROW_NUMBER() OVER (ORDER BY s.priority) AS rn
  FROM bom_line_item_substitutes s, source_line_item si
  WHERE s.bom_line_item_id = si.id
)
INSERT INTO bom_line_item_substitutes (bom_line_item_id, part_id, priority)
SELECT $1, ss.part_id, me.max_p + ss.rn
FROM source_subs ss, max_existing me
ON CONFLICT (bom_line_item_id, part_id) DO NOTHING;

-- name: ListRevisionsWithSubstitutesForPart :many
-- Find revisions in the same project where this resolved_part_id had substitutes
-- (used for "Import from Previous" dropdown)
SELECT br.id, br.label, br.imported_at
FROM bom_revisions br
JOIN bom_line_items bli ON bli.bom_revision_id = br.id
JOIN bom_line_item_substitutes s ON s.bom_line_item_id = bli.id
WHERE br.project_id = $2
  AND bli.resolved_part_id = $1
  AND br.id != $3
GROUP BY br.id, br.label, br.imported_at
ORDER BY br.imported_at DESC;

-- name: SetBuildSubstituteSelection :exec
INSERT INTO build_substitute_selections (build_id, bom_line_item_id, selected_part_id)
VALUES ($1, $2, $3)
ON CONFLICT (build_id, bom_line_item_id) DO UPDATE SET selected_part_id = EXCLUDED.selected_part_id;

-- name: GetBuildSubstituteSelections :many
SELECT bss.bom_line_item_id, bss.selected_part_id, p.mpn AS selected_part_mpn
FROM build_substitute_selections bss
JOIN parts p ON p.id = bss.selected_part_id
WHERE bss.build_id = $1;

-- name: DeleteBuildSubstituteSelections :exec
DELETE FROM build_substitute_selections WHERE build_id = $1;

-- name: DeleteBuildSubstituteSelection :exec
-- Remove a single line item's selection (revert it to the primary part)
DELETE FROM build_substitute_selections
WHERE build_id = $1 AND bom_line_item_id = $2;

-- name: SetBuildSelectionsCheckedAt :exec
UPDATE builds SET selections_checked_at = $2 WHERE id = $1;

-- name: IsBuildSelectionStale :one
-- Returns true if any stock adjustment for BOM parts is newer than selections_checked_at
SELECT EXISTS (
  SELECT 1
  FROM stock_adjustments sa
  JOIN inventory_entries ie ON ie.id = sa.inventory_entry_id
  JOIN bom_line_items bli ON bli.resolved_part_id = ie.part_id
  WHERE bli.bom_revision_id = $1
    AND sa.adjusted_at > $2
) AS stale;

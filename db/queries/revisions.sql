-- name: ListRevisions :many
SELECT
    br.id, br.label, br.imported_at, br.notes,
    u.display_name AS imported_by_display_name,
    COUNT(bli.id) AS item_count,
    COUNT(bli.id) FILTER (WHERE bli.resolved_part_id IS NOT NULL) AS resolved_count,
    COUNT(bli.id) FILTER (WHERE bli.resolved_part_id IS NULL) AS unresolved_count,
    CASE WHEN p.active_bom_revision_id = br.id THEN TRUE ELSE FALSE END AS is_active
FROM bom_revisions br
JOIN users u ON u.id = br.imported_by
JOIN projects p ON p.id = br.project_id
LEFT JOIN bom_line_items bli ON bli.bom_revision_id = br.id
WHERE br.project_id = $1
GROUP BY br.id, u.display_name, p.active_bom_revision_id
ORDER BY br.imported_at DESC;
 
-- name: GetRevision :one
SELECT
    br.id, br.label, br.imported_at, br.notes,
    u.display_name AS imported_by_display_name,
    CASE WHEN p.active_bom_revision_id = br.id THEN TRUE ELSE FALSE END AS is_active
FROM bom_revisions br
JOIN users u ON u.id = br.imported_by
JOIN projects p ON p.id = br.project_id
WHERE br.id = $1 AND br.project_id = $2;
 
-- name: CreateRevision :one
INSERT INTO bom_revisions (project_id, label, notes, imported_by)
VALUES ($1, $2, $3, $4)
RETURNING id, project_id, label, imported_at, notes, imported_by;
 
-- name: UpdateRevision :one
UPDATE bom_revisions
SET label = COALESCE(sqlc.narg('label'), label),
    notes = COALESCE(sqlc.narg('notes'), notes)
WHERE id = sqlc.arg('id') AND project_id = sqlc.arg('project_id')
RETURNING id, label, imported_at, notes, imported_by;

-- name: CreateBOMLineItem :one
INSERT INTO bom_line_items (
    bom_revision_id, reference_designators, quantity, raw_description,
    manufacturer, mpn, part_name, sku, distributor_name, resolved_distributor_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id;

-- name: ListBOMLineItems :many
SELECT
    bli.id, bli.reference_designators, bli.quantity, bli.raw_description,
    bli.resolved_part_id,
    bli.part_name, bli.manufacturer, bli.mpn, bli.sku,
    COALESCE(d.name, bli.distributor_name, '') AS distributor_name,
    bli.resolved_distributor_id,
    bli.resolution_state, bli.ignored,
    p.mpn AS resolved_part_mpn
FROM bom_line_items bli
LEFT JOIN parts p ON p.id = bli.resolved_part_id
LEFT JOIN distributors d ON d.id = bli.resolved_distributor_id
WHERE bli.bom_revision_id = $1
ORDER BY bli.id;

-- name: ListBOMLineItemsWithResolveData :many
-- Like ListBOMLineItems but also joins the linked part_resolve_item so the
-- revision page can render the segmented progress bar and the new-part review
-- surface without extra round-trips.
SELECT
    bli.id, bli.reference_designators, bli.quantity, bli.raw_description,
    bli.resolved_part_id,
    bli.part_name, bli.manufacturer, bli.mpn, bli.sku,
    COALESCE(d.name, bli.distributor_name, '') AS distributor_name,
    bli.resolved_distributor_id,
    bli.resolution_state, bli.ignored,
    p.mpn AS resolved_part_mpn,
    pn.flag AS resolved_part_flag,
    bli.resolve_item_id,
    pri.status          AS resolve_status,
    pri.task_type       AS resolve_task_type,
    pri.enrichment_data AS resolve_enrichment_data,
    pri.error_message   AS resolve_error_message
FROM bom_line_items bli
LEFT JOIN parts p ON p.id = bli.resolved_part_id
LEFT JOIN distributors d ON d.id = bli.resolved_distributor_id
LEFT JOIN part_resolve_items pri ON pri.id = bli.resolve_item_id
LEFT JOIN part_notes pn ON pn.part_id = bli.resolved_part_id AND pn.team_id = sqlc.arg('team_id')
WHERE bli.bom_revision_id = sqlc.arg('bom_revision_id')
ORDER BY bli.id;

-- name: SetBOMLineItemResolveItem :exec
-- Links a line item to its in-flight resolve item and marks it importing.
UPDATE bom_line_items
SET resolve_item_id  = @resolve_item_id,
    resolution_state = @resolution_state
WHERE id = @id;

-- name: SetBOMLineItemResolutionState :exec
-- Sets just the resolution_state (e.g. import_ambiguous / import_no_result),
-- used by the resolve worker when it cannot auto-commit.
UPDATE bom_line_items
SET resolution_state = @resolution_state
WHERE id = @id;

-- name: LinkBOMLineItemPart :one
-- Resolves a line item to a part by id only (the resolve commit path does not
-- carry the revision id). Returns context for follow-up actions.
UPDATE bom_line_items
SET resolved_part_id = @resolved_part_id,
    resolution_state = @resolution_state
WHERE id = @id
RETURNING id, bom_revision_id;

-- name: GetBOMLineItemContext :one
-- Returns the revision + project for a line item, for substitute inheritance
-- and revision_changed events from the resolve worker.
SELECT bli.id, bli.bom_revision_id, br.project_id
FROM bom_line_items bli
JOIN bom_revisions br ON br.id = bli.bom_revision_id
WHERE bli.id = $1;

-- name: GetBOMLineItem :one
SELECT id, bom_revision_id, reference_designators, quantity, raw_description, resolved_part_id
FROM bom_line_items
WHERE id = $1 AND bom_revision_id = $2;

-- name: UpdateBOMLineItem :one
UPDATE bom_line_items
SET resolved_part_id = sqlc.narg('resolved_part_id'),
    resolution_state = sqlc.narg('resolution_state')
WHERE id = sqlc.arg('id') AND bom_revision_id = sqlc.arg('bom_revision_id')
RETURNING id, reference_designators, quantity, raw_description,
          resolved_part_id, resolution_state;

-- name: ListAllSubstitutesForRevision :many
SELECT
    s.bom_line_item_id,
    s.part_id,
    p.mpn AS part_mpn,
    s.priority
FROM bom_line_item_substitutes s
JOIN parts p ON p.id = s.part_id
JOIN bom_line_items bli ON bli.id = s.bom_line_item_id
WHERE bli.bom_revision_id = $1
ORDER BY s.bom_line_item_id, s.priority ASC;

-- name: AddBOMSubstitute :exec
INSERT INTO bom_line_item_substitutes (bom_line_item_id, part_id)
VALUES ($1, $2);

-- name: RemoveBOMSubstitute :exec
DELETE FROM bom_line_item_substitutes
WHERE bom_line_item_id = $1 AND part_id = $2;

-- name: IgnoreBOMLineItem :one
UPDATE bom_line_items
SET ignored = sqlc.arg('ignored'),
    resolved_part_id = CASE WHEN sqlc.arg('ignored')::boolean THEN NULL ELSE resolved_part_id END,
    resolution_state = CASE WHEN sqlc.arg('ignored')::boolean THEN 'ignored' ELSE NULL END
WHERE id = sqlc.arg('id') AND bom_revision_id = sqlc.arg('bom_revision_id')
RETURNING id, reference_designators, quantity, raw_description,
          resolved_part_id, resolution_state, ignored;

-- name: GetRevisionProjectID :one
SELECT project_id FROM bom_revisions WHERE id = $1;

-- name: GetPriorRevisionPartByMPNAndManufacturer :one
SELECT bli.resolved_part_id
FROM bom_line_items bli
JOIN bom_revisions br ON br.id = bli.bom_revision_id
WHERE br.project_id        = @project_id
  AND br.id               != @current_revision_id
  AND bli.mpn              = @mpn
  AND bli.manufacturer ILIKE @manufacturer
  AND bli.resolved_part_id IS NOT NULL
ORDER BY br.imported_at DESC
LIMIT 1;

-- name: GetPriorRevisionPartBySKU :one
SELECT bli.resolved_part_id
FROM bom_line_items bli
JOIN bom_revisions br ON br.id = bli.bom_revision_id
WHERE br.project_id              = @project_id
  AND br.id                     != @current_revision_id
  AND bli.sku                    = @sku
  AND bli.resolved_distributor_id = @distributor_id
  AND bli.resolved_part_id IS NOT NULL
ORDER BY br.imported_at DESC
LIMIT 1;

-- name: GetImmediatelyPriorRevision :one
-- Returns the most recently imported revision in the same project, excluding the current one.
-- Used to determine which revision's substitutes to inherit during BOM import.
SELECT id FROM bom_revisions
WHERE project_id = @project_id
  AND id != @current_revision_id
ORDER BY imported_at DESC
LIMIT 1;

-- name: GetInventoryQuantitiesForRevision :many
SELECT ie.part_id, ie.quantity_on_hand, ie.minimum_stock_quantity, ie.reorder_quantity
FROM inventory_entries ie
WHERE ie.team_id = $2
  AND ie.part_id IN (
    SELECT bm.resolved_part_id FROM bom_line_items bm
    WHERE bm.bom_revision_id = $1 AND bm.resolved_part_id IS NOT NULL
    UNION
    SELECT s.part_id FROM bom_line_item_substitutes s
    JOIN bom_line_items bli ON bli.id = s.bom_line_item_id
    WHERE bli.bom_revision_id = $1
  );

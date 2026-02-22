-- name: ListBuilds :many
SELECT
    b.id, b.project_id, pr.name AS project_name,
    b.bom_revision_id, br.label AS bom_revision_label,
    b.boards_built, b.built_at, b.notes,
    u.display_name AS recorded_by_display_name
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
JOIN users u ON u.id = b.recorded_by
WHERE b.team_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR b.project_id = sqlc.narg('project_id'))
ORDER BY b.built_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountBuilds :one
SELECT COUNT(*) FROM builds
WHERE team_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'));

-- name: CreateBuild :one
INSERT INTO builds (team_id, project_id, bom_revision_id, boards_built, notes, recorded_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, team_id, project_id, bom_revision_id, boards_built, built_at, notes, recorded_by;

-- name: GetBuild :one
SELECT
    b.id, b.team_id, b.project_id, pr.name AS project_name,
    b.bom_revision_id, br.label AS bom_revision_label,
    b.boards_built, b.built_at, b.notes,
    u.display_name AS recorded_by_display_name
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
JOIN users u ON u.id = b.recorded_by
WHERE b.id = $1 AND b.team_id = $2;

-- name: ListBuildDeductions :many
SELECT
    sa.id, sa.inventory_entry_id, sa.delta,
    ie.part_id, p.mpn AS part_mpn
FROM stock_adjustments sa
JOIN inventory_entries ie ON ie.id = sa.inventory_entry_id
JOIN parts p ON p.id = ie.part_id
WHERE sa.build_id = $1
ORDER BY sa.id;

-- name: CreateBuildStockAdjustment :one
INSERT INTO stock_adjustments (inventory_entry_id, delta, reason, created_by, build_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, inventory_entry_id, delta, adjusted_at, reason, created_by, build_id;

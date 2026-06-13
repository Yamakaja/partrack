-- name: ListProjects :many
SELECT
    p.id, p.name, p.description, p.status,
    p.active_bom_revision_id,
    br.label AS active_bom_revision_label,
    p.preferred_distributor_id,
    p.source_url,
    p.project_manager_id,
    pm.display_name AS project_manager_display_name,
    u.display_name AS created_by_display_name,
    p.created_at
FROM projects p
LEFT JOIN bom_revisions br ON br.id = p.active_bom_revision_id
JOIN users u ON u.id = p.created_by
LEFT JOIN users pm ON pm.id = p.project_manager_id
WHERE p.team_id = $1
  AND (sqlc.narg('status')::project_status IS NULL OR p.status = sqlc.narg('status'))
ORDER BY p.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountProjects :one
SELECT COUNT(*) FROM projects
WHERE team_id = $1
  AND (sqlc.narg('status')::project_status IS NULL OR status = sqlc.narg('status'));

-- name: CreateProject :one
INSERT INTO projects (team_id, name, description, preferred_distributor_id, source_url, project_manager_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, team_id, name, description, status, active_bom_revision_id,
          bom_import_config, preferred_distributor_id, source_url, project_manager_id,
          created_by, created_at;

-- name: GetProjectNote :one
-- Team-scoped: returns no rows (→ 404) if the project isn't owned by the team.
SELECT notes, notes_updated_by, notes_updated_at
FROM projects
WHERE id = $1 AND team_id = $2;

-- name: UpdateProjectNote :one
UPDATE projects
SET notes = $3, notes_updated_by = $4, notes_updated_at = NOW()
WHERE id = $1 AND team_id = $2
RETURNING notes, notes_updated_by, notes_updated_at;

-- name: GetProject :one
SELECT
    p.id, p.name, p.description, p.status,
    p.active_bom_revision_id,
    br.label AS active_bom_revision_label,
    p.bom_import_config,
    p.preferred_distributor_id,
    p.source_url,
    p.project_manager_id,
    pm.display_name AS project_manager_display_name,
    u.display_name AS created_by_display_name,
    p.created_at,
    rs.item_count AS rev_item_count,
    rs.resolved_count AS rev_resolved_count,
    rs.unresolved_count AS rev_unresolved_count,
    rs.ambiguous_count AS rev_ambiguous_count
FROM projects p
LEFT JOIN bom_revisions br ON br.id = p.active_bom_revision_id
JOIN users u ON u.id = p.created_by
LEFT JOIN users pm ON pm.id = p.project_manager_id
LEFT JOIN LATERAL (
    SELECT
        COUNT(*)::bigint AS item_count,
        COUNT(*) FILTER (WHERE resolved_part_id IS NOT NULL)::bigint AS resolved_count,
        COUNT(*) FILTER (WHERE resolved_part_id IS NULL)::bigint AS unresolved_count,
        COUNT(*) FILTER (WHERE resolution_state = 'ambiguous')::bigint AS ambiguous_count
    FROM bom_line_items
    WHERE bom_revision_id = p.active_bom_revision_id
) rs ON TRUE
WHERE p.id = $1 AND p.team_id = $2;

-- name: UpdateProject :one
UPDATE projects
SET name                     = COALESCE(sqlc.narg('name'), name),
    description              = COALESCE(sqlc.narg('description'), description),
    status                   = COALESCE(sqlc.narg('status')::project_status, status),
    active_bom_revision_id   = COALESCE(sqlc.narg('active_bom_revision_id'), active_bom_revision_id),
    preferred_distributor_id = COALESCE(sqlc.narg('preferred_distributor_id'), preferred_distributor_id),
    source_url               = COALESCE(sqlc.narg('source_url'), source_url),
    project_manager_id       = COALESCE(sqlc.narg('project_manager_id'), project_manager_id),
    bom_import_config        = sqlc.arg('bom_import_config')
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, name, description, status, active_bom_revision_id, bom_import_config,
          preferred_distributor_id, source_url, project_manager_id, created_by, created_at;

-- name: UpdateProjectKeepConfig :one
UPDATE projects
SET name                     = COALESCE(sqlc.narg('name'), name),
    description              = COALESCE(sqlc.narg('description'), description),
    status                   = COALESCE(sqlc.narg('status')::project_status, status),
    active_bom_revision_id   = COALESCE(sqlc.narg('active_bom_revision_id'), active_bom_revision_id),
    preferred_distributor_id = COALESCE(sqlc.narg('preferred_distributor_id'), preferred_distributor_id),
    source_url               = COALESCE(sqlc.narg('source_url'), source_url),
    project_manager_id       = COALESCE(sqlc.narg('project_manager_id'), project_manager_id)
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, name, description, status, active_bom_revision_id, bom_import_config,
          preferred_distributor_id, source_url, project_manager_id, created_by, created_at;

-- name: SetProjectActiveBOM :exec
UPDATE projects
SET active_bom_revision_id = $2
WHERE id = $1;

-- name: SetProjectBOMImportConfig :exec
UPDATE projects
SET bom_import_config = $2
WHERE id = $1;

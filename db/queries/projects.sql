-- name: ListProjects :many
SELECT
    p.id, p.name, p.description, p.status,
    p.active_bom_revision_id,
    br.label AS active_bom_revision_label,
    u.display_name AS created_by_display_name,
    p.created_at
FROM projects p
LEFT JOIN bom_revisions br ON br.id = p.active_bom_revision_id
JOIN users u ON u.id = p.created_by
WHERE p.team_id = $1
  AND (sqlc.narg('status')::project_status IS NULL OR p.status = sqlc.narg('status'))
ORDER BY p.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountProjects :one
SELECT COUNT(*) FROM projects
WHERE team_id = $1
  AND (sqlc.narg('status')::project_status IS NULL OR status = sqlc.narg('status'));

-- name: CreateProject :one
INSERT INTO projects (team_id, name, description, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id, team_id, name, description, status, active_bom_revision_id,
          bom_import_config, created_by, created_at;

-- name: GetProject :one
SELECT
    p.id, p.name, p.description, p.status,
    p.active_bom_revision_id,
    br.label AS active_bom_revision_label,
    p.bom_import_config,
    u.display_name AS created_by_display_name,
    p.created_at
FROM projects p
LEFT JOIN bom_revisions br ON br.id = p.active_bom_revision_id
JOIN users u ON u.id = p.created_by
WHERE p.id = $1 AND p.team_id = $2;

-- name: UpdateProject :one
UPDATE projects
SET name                   = COALESCE(sqlc.narg('name'), name),
    description            = COALESCE(sqlc.narg('description'), description),
    status                 = COALESCE(sqlc.narg('status')::project_status, status),
    active_bom_revision_id = COALESCE(sqlc.narg('active_bom_revision_id'), active_bom_revision_id),
    bom_import_config      = sqlc.arg('bom_import_config')
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, name, description, status, active_bom_revision_id, bom_import_config, created_by, created_at;

-- name: UpdateProjectKeepConfig :one
UPDATE projects
SET name                   = COALESCE(sqlc.narg('name'), name),
    description            = COALESCE(sqlc.narg('description'), description),
    status                 = COALESCE(sqlc.narg('status')::project_status, status),
    active_bom_revision_id = COALESCE(sqlc.narg('active_bom_revision_id'), active_bom_revision_id)
WHERE id = sqlc.arg('id') AND team_id = sqlc.arg('team_id')
RETURNING id, name, description, status, active_bom_revision_id, bom_import_config, created_by, created_at;

-- name: SetProjectActiveBOM :exec
UPDATE projects
SET active_bom_revision_id = $2
WHERE id = $1;

-- name: SetProjectBOMImportConfig :exec
UPDATE projects
SET bom_import_config = $2
WHERE id = $1;

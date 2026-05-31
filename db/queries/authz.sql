-- Ownership-resolution queries used by handler guards to confirm that a nested
-- resource (project / revision / BOM line item) belongs to the team named in the
-- URL. RequireTeamRole only proves membership in {teamId}; these anchor the rest
-- of the path chain to that team. They return the owning team_id so the result
-- can be cached in the immutable resource-owner cache (these relationships never
-- change). pgx.ErrNoRows ⇒ the resource does not exist ⇒ handler returns 404.

-- name: ProjectTeam :one
SELECT team_id FROM projects WHERE id = $1;

-- name: RevisionTeam :one
SELECT p.team_id
FROM bom_revisions br
JOIN projects p ON p.id = br.project_id
WHERE br.id = $1;

-- name: BOMLineItemTeam :one
SELECT p.team_id
FROM bom_line_items bli
JOIN bom_revisions br ON br.id = bli.bom_revision_id
JOIN projects p ON p.id = br.project_id
WHERE bli.id = $1;

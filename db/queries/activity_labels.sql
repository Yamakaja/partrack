-- Narrow lookups feeding activity-log subject labels (formatted by
-- internal/activity/label.go). Kept apart from the full domain fetches so the
-- write path pays for one indexed read rather than a wide join it won't use.

-- name: GetPartLabelFields :one
SELECT mpn, short_description
FROM parts
WHERE id = $1;

-- name: GetOrderLabelFields :one
SELECT d.name AS distributor_name, o.purchase_order_number, o.external_reference
FROM orders o
JOIN distributors d ON d.id = o.distributor_id
WHERE o.id = $1 AND o.team_id = $2;

-- name: GetBuildLabelFields :one
SELECT pr.name AS project_name, br.label AS bom_revision_label
FROM builds b
JOIN projects pr ON pr.id = b.project_id
JOIN bom_revisions br ON br.id = b.bom_revision_id
WHERE b.id = $1 AND b.team_id = $2;

-- name: GetProjectNameForLabel :one
SELECT name
FROM projects
WHERE id = $1;

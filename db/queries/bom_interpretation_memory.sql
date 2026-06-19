-- name: UpsertInterpretationMemory :exec
-- Records a user-confirmed interpretation for a project, keyed by the normalised
-- (class, value, footprint). Last write wins. Written from the import review
-- gate; consulted on every later import of the SAME project (see doc/bom-import.md §9.2).
INSERT INTO bom_interpretation_memory (
    project_id, class, value_key, footprint_key, interpretation, created_by
) VALUES (@project_id, @class, @value_key, @footprint_key, @interpretation, @created_by)
ON CONFLICT (project_id, class, value_key, footprint_key)
DO UPDATE SET interpretation = EXCLUDED.interpretation,
              created_by     = EXCLUDED.created_by,
              updated_at     = now();

-- name: ListInterpretationMemory :many
-- All remembered interpretations for a project, most-recently-confirmed first. The
-- import path loads these once and builds an in-memory lookup. Bounded so a
-- pathological project cannot make import scan unboundedly.
SELECT class, value_key, footprint_key, interpretation
FROM bom_interpretation_memory
WHERE project_id = $1
ORDER BY updated_at DESC
LIMIT 5000;

-- name: ListInterpretationMemoryEntries :many
-- Full detail for the project management UI (vs the capped, 4-column import-path
-- ListInterpretationMemory): every remembered interpretation with provenance, so
-- a user can inspect and prune what the import will reuse.
SELECT m.id, m.class, m.value_key, m.footprint_key, m.interpretation,
       m.created_by, u.display_name AS created_by_name,
       m.created_at, m.updated_at
FROM bom_interpretation_memory m
LEFT JOIN users u ON u.id = m.created_by
WHERE m.project_id = $1
ORDER BY m.updated_at DESC;

-- name: DeleteInterpretationMemoryEntry :execrows
-- Project-scoped delete (the project_id filter closes cross-tenant IDOR at the
-- data layer). Returns rows affected so the handler can 404 a stale/foreign id.
DELETE FROM bom_interpretation_memory
WHERE id = @id AND project_id = @project_id;

-- name: ListBOMLineItemInterpretations :many
-- The persisted interpretations of one revision's line items, used to carry
-- interpretations forward into the next revision of the same project.
SELECT reference_designators, value, footprint, interpretation, interpretation_source
FROM bom_line_items
WHERE bom_revision_id = $1;

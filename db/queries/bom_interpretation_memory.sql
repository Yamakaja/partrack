-- name: UpsertInterpretationMemory :exec
-- Records a user-confirmed interpretation for a team, keyed by the normalised
-- (class, value, footprint). Last write wins. Written from the import review
-- gate; consulted on every later import (see doc/bom-import.md §9.2).
INSERT INTO bom_interpretation_memory (
    team_id, class, value_key, footprint_key, interpretation, created_by
) VALUES (@team_id, @class, @value_key, @footprint_key, @interpretation, @created_by)
ON CONFLICT (team_id, class, value_key, footprint_key)
DO UPDATE SET interpretation = EXCLUDED.interpretation,
              created_by     = EXCLUDED.created_by,
              updated_at     = now();

-- name: ListInterpretationMemory :many
-- All remembered interpretations for a team, most-recently-confirmed first. The
-- import path loads these once and builds an in-memory lookup. Bounded so a
-- pathological team cannot make import scan unboundedly.
SELECT class, value_key, footprint_key, interpretation
FROM bom_interpretation_memory
WHERE team_id = $1
ORDER BY updated_at DESC
LIMIT 5000;

-- name: ListBOMLineItemInterpretations :many
-- The persisted interpretations of one revision's line items, used to carry
-- interpretations forward into the next revision of the same project.
SELECT reference_designators, value, footprint, interpretation, interpretation_source
FROM bom_line_items
WHERE bom_revision_id = $1;

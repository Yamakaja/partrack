-- Part collections ("Lists"): user-private bags of parts. Every query that
-- addresses a collection filters by owner (user_id + team_id) so cross-user
-- access fails at the data layer (ErrNoRows -> 404), never in handler logic.

-- name: CreatePartCollection :one
INSERT INTO part_collections (user_id, team_id, name)
VALUES ($1, $2, $3)
RETURNING id, user_id, team_id, name, archived, collect_capture_qty, created_at, updated_at;

-- name: ListPartCollections :many
SELECT pc.id, pc.name, pc.archived, pc.collect_capture_qty, pc.created_at, pc.updated_at,
       COUNT(pci.id)::int                                        AS item_count,
       COALESCE(SUM(pci.quantity), 0)::bigint                    AS total_quantity,
       COUNT(pci.id) FILTER (WHERE pci.quantity IS NULL)::int    AS unquantified_count,
       COUNT(pci.id) FILTER (WHERE pci.checked_at IS NOT NULL)::int AS checked_count
FROM part_collections pc
LEFT JOIN part_collection_items pci ON pci.collection_id = pc.id
WHERE pc.user_id = $1
  AND pc.team_id = $2
  AND (sqlc.arg('include_archived')::bool OR pc.archived = FALSE)
GROUP BY pc.id
ORDER BY pc.updated_at DESC;

-- name: GetPartCollection :one
SELECT id, user_id, team_id, name, archived, collect_capture_qty, created_at, updated_at
FROM part_collections
WHERE id = $1 AND user_id = $2 AND team_id = $3;

-- name: UpdatePartCollection :one
UPDATE part_collections
SET name                = COALESCE(sqlc.narg('name'), name),
    archived            = COALESCE(sqlc.narg('archived'), archived),
    collect_capture_qty = COALESCE(sqlc.narg('collect_capture_qty'), collect_capture_qty),
    updated_at          = now()
WHERE id = $1 AND user_id = $2 AND team_id = $3
RETURNING id, user_id, team_id, name, archived, collect_capture_qty, created_at, updated_at;

-- name: DeletePartCollection :execrows
DELETE FROM part_collections
WHERE id = $1 AND user_id = $2 AND team_id = $3;

-- name: TouchPartCollection :exec
UPDATE part_collections SET updated_at = now() WHERE id = $1;

-- Add = upsert with the collection merge rule: an incoming quantity sums onto
-- the existing one (NULL existing counts as 0); a bare add (NULL quantity)
-- leaves the existing quantity untouched. An incoming note replaces the note.
-- name: UpsertPartCollectionItem :one
INSERT INTO part_collection_items (collection_id, part_id, quantity, note)
VALUES ($1, $2, sqlc.narg('quantity'), sqlc.narg('note'))
ON CONFLICT (collection_id, part_id) DO UPDATE
SET quantity = CASE
        WHEN EXCLUDED.quantity IS NULL THEN part_collection_items.quantity
        ELSE COALESCE(part_collection_items.quantity, 0) + EXCLUDED.quantity
    END,
    note = COALESCE(EXCLUDED.note, part_collection_items.note)
RETURNING id, collection_id, part_id, quantity, note, checked_at;

-- Set-style item edit: explicit flags so quantity/note can be cleared to NULL
-- (a COALESCE-narg update could never write NULL back).
-- name: UpdatePartCollectionItem :one
UPDATE part_collection_items
SET quantity   = CASE WHEN sqlc.arg('set_quantity')::bool THEN sqlc.narg('quantity') ELSE quantity END,
    note       = CASE WHEN sqlc.arg('set_note')::bool THEN sqlc.narg('note') ELSE note END,
    -- Checking an already-checked row keeps the original timestamp, so a
    -- redundant write never reorders "most recently checked".
    checked_at = CASE
        WHEN NOT sqlc.arg('set_checked')::bool THEN checked_at
        WHEN sqlc.arg('checked')::bool         THEN COALESCE(checked_at, now())
        ELSE NULL
    END
WHERE id = $1 AND collection_id = $2
RETURNING id, collection_id, part_id, quantity, note, checked_at;

-- name: DeletePartCollectionItem :execrows
DELETE FROM part_collection_items
WHERE id = $1 AND collection_id = $2;

-- Empty the list in one statement (the "Clear items" verb); the collection row survives.
-- name: ClearPartCollectionItems :execrows
DELETE FROM part_collection_items
WHERE collection_id = $1;

-- Scoped delete: the "Delete selected" verb. Ids outside the collection are
-- silently ignored (the collection_id predicate is the authorization).
-- name: DeletePartCollectionItemsByIDs :execrows
DELETE FROM part_collection_items
WHERE collection_id = $1 AND id = ANY(sqlc.arg('item_ids')::uuid[]);

-- The selection ops behind the header checkbox and its menu. 'all'/'none'/
-- 'invert' sweep the whole collection; 'set' writes @checked to @item_ids only
-- (row toggles and shift-click ranges). Checking preserves an existing
-- checked_at for the same reason UpdatePartCollectionItem does.
-- name: SetPartCollectionItemsChecked :execrows
UPDATE part_collection_items
SET checked_at = CASE
        WHEN sqlc.arg('op')::text = 'none'                              THEN NULL
        WHEN sqlc.arg('op')::text = 'invert'                            THEN (CASE WHEN checked_at IS NULL THEN now() ELSE NULL END)
        WHEN sqlc.arg('op')::text = 'set' AND NOT sqlc.arg('checked')::bool THEN NULL
        ELSE COALESCE(checked_at, now())
    END
WHERE collection_id = sqlc.arg('collection_id')
  AND (sqlc.arg('op')::text <> 'set' OR id = ANY(sqlc.arg('item_ids')::uuid[]));

-- Phone list view: rows with team stock + storage location — the "where do
-- these parts live?" lens. The phone resolves location labels from its cached
-- storage tree, so ids + coordinates suffice.
-- name: ListPartCollectionItemsWithStock :many
SELECT pci.id, pci.part_id, pci.quantity, pci.note, pci.checked_at,
       p.mpn, COALESCE(p.short_description, p.description, '') AS part_description,
       ie.quantity_on_hand, ie.storage_location_id, ie.storage_coordinate
FROM part_collection_items pci
JOIN parts p ON p.id = pci.part_id
LEFT JOIN inventory_entries ie
       ON ie.part_id = pci.part_id AND ie.team_id = sqlc.arg('team_id')
WHERE pci.collection_id = sqlc.arg('collection_id')
ORDER BY pci.id;

-- Running tally for one collection (scan Collect responses + summaries).
-- name: GetPartCollectionTally :one
SELECT COUNT(pci.id)::int                                     AS item_count,
       COALESCE(SUM(pci.quantity), 0)::bigint                 AS total_quantity,
       COUNT(pci.id) FILTER (WHERE pci.quantity IS NULL)::int AS unquantified_count,
       COUNT(pci.id) FILTER (WHERE pci.checked_at IS NOT NULL)::int AS checked_count
FROM part_collection_items pci
WHERE pci.collection_id = $1;

-- Panel rows: the PartCell render contract (mpn, manufacturer, description,
-- lifecycle) joined in; everything richer comes from the part hover card's
-- lazy fetch. uuidv7 PK order = insertion order.
-- name: ListPartCollectionItems :many
SELECT pci.id, pci.part_id, pci.quantity, pci.note, pci.checked_at,
       p.mpn, p.manufacturer, p.lifecycle_status, p.image_url,
       COALESCE(p.short_description, p.description, '') AS part_description,
       ie.quantity_on_hand
FROM part_collection_items pci
JOIN parts p ON p.id = pci.part_id
LEFT JOIN inventory_entries ie
       ON ie.part_id = pci.part_id AND ie.team_id = sqlc.arg('team_id')
WHERE pci.collection_id = sqlc.arg('collection_id')
ORDER BY pci.id;

-- Pick mode's membership test: is the scanned part on this list, and is it
-- already checked? (The uniqueness of (collection_id, part_id) makes this the
-- row the scan would tick.)
-- name: GetPartCollectionItemByPart :one
SELECT id, collection_id, part_id, quantity, note, checked_at
FROM part_collection_items
WHERE collection_id = $1 AND part_id = $2;

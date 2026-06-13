-- Parts catalog, parameters, and distributor listing queries.

-- name: ListPartCategoryCounts :many
-- Returns direct part counts per category for the team's catalog
-- (global parts + team-scoped parts). Used to populate the category filter picker.
SELECT category_id, COUNT(*)::int AS count
FROM parts
WHERE (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND deleted_at IS NULL
  AND category_id IS NOT NULL
GROUP BY category_id;

-- name: ListParts :many
WITH RECURSIVE cat_tree AS (
  SELECT id FROM categories WHERE id = sqlc.narg('category_id')
  UNION ALL
  SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
)
SELECT id, scope, team_id, mpn, manufacturer, description, category, category_id, category_source,
       package, lifecycle_status, created_at
FROM parts
WHERE (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR description ILIKE '%' || sqlc.narg('q') || '%'
       OR manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('mpn_exact')::text IS NULL OR mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category_id')::int IS NULL OR category_id IN (SELECT id FROM cat_tree))
  AND (sqlc.narg('package')::text IS NULL OR package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::part_lifecycle_status IS NULL
       OR lifecycle_status = sqlc.narg('lifecycle_status'))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR scope = sqlc.narg('filter_scope'))
ORDER BY mpn NULLS LAST, id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountParts :one
WITH RECURSIVE cat_tree AS (
  SELECT id FROM categories WHERE id = sqlc.narg('category_id')
  UNION ALL
  SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
)
SELECT COUNT(*) FROM parts
WHERE (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR description ILIKE '%' || sqlc.narg('q') || '%'
       OR manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('mpn_exact')::text IS NULL OR mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category_id')::int IS NULL OR category_id IN (SELECT id FROM cat_tree))
  AND (sqlc.narg('package')::text IS NULL OR package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::part_lifecycle_status IS NULL
       OR lifecycle_status = sqlc.narg('lifecycle_status'))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR scope = sqlc.narg('filter_scope'));

-- name: ListPartsWithInventory :many
-- Like ListParts but LEFT JOINs the team's inventory entry for each part.
-- All inventory fields are NULL for parts that are not tracked by the team.
WITH RECURSIVE cat_tree AS (
  SELECT id FROM categories WHERE id = sqlc.narg('category_id')
  UNION ALL
  SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
),
loc_tree AS (
  SELECT id FROM storage_locations WHERE id = sqlc.narg('storage_location_id')::uuid
  UNION ALL
  SELECT sl.id FROM storage_locations sl JOIN loc_tree lt ON sl.parent_id = lt.id
)
SELECT
    p.id, p.scope, p.team_id, p.mpn, p.manufacturer, p.description, p.short_description,
    p.category, p.category_id, p.category_source, p.package, p.lifecycle_status, p.created_at,
    c.display_name   AS category_name,
    ie.id            AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.minimum_stock_quantity,
    ie.reorder_quantity,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label         AS storage_location_label
FROM parts p
LEFT JOIN categories c ON c.id = p.category_id
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = sqlc.narg('team_id')))
  AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.short_description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%'
       OR EXISTS (SELECT 1 FROM part_aliases pa WHERE pa.part_id = p.id AND pa.alias ILIKE '%' || sqlc.narg('q') || '%'))
  AND (sqlc.narg('mpn_exact')::text IS NULL OR p.mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category_id')::int IS NULL OR p.category_id IN (SELECT id FROM cat_tree))
  AND (sqlc.narg('package')::text IS NULL OR p.package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::text IS NULL
       OR p.lifecycle_status = ANY(string_to_array(sqlc.narg('lifecycle_status'), ',')::part_lifecycle_status[]))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR p.scope = sqlc.narg('filter_scope'))
  AND (
      sqlc.narg('stock_status')::text IS NULL
      OR ('in_stock'     = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand > 0)
      OR ('low_stock'    = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand >= 0 AND ie.minimum_stock_quantity IS NOT NULL AND ie.quantity_on_hand < ie.minimum_stock_quantity)
      OR ('out_of_stock' = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NOT NULL AND ie.quantity_on_hand = 0)
      OR ('not_tracked'  = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NULL)
  )
  AND (sqlc.narg('part_id_filter')::text IS NULL
       OR p.id = ANY(string_to_array(sqlc.narg('part_id_filter'), ',')::uuid[]))
  AND (sqlc.narg('storage_location_id')::uuid IS NULL
       OR ie.storage_location_id IN (SELECT id FROM loc_tree))
  AND (sqlc.narg('storage_cells')::text IS NULL
       OR array_to_string(ie.storage_coordinate, ',') = ANY(string_to_array(sqlc.narg('storage_cells'), '|')))
ORDER BY p.mpn NULLS LAST, p.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountPartsWithInventory :one
WITH RECURSIVE cat_tree AS (
  SELECT id FROM categories WHERE id = sqlc.narg('category_id')
  UNION ALL
  SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
),
loc_tree AS (
  SELECT id FROM storage_locations WHERE id = sqlc.narg('storage_location_id')::uuid
  UNION ALL
  SELECT sl.id FROM storage_locations sl JOIN loc_tree lt ON sl.parent_id = lt.id
)
SELECT COUNT(*) FROM parts p
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
WHERE (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = sqlc.narg('team_id')))
  AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.short_description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%'
       OR EXISTS (SELECT 1 FROM part_aliases pa WHERE pa.part_id = p.id AND pa.alias ILIKE '%' || sqlc.narg('q') || '%'))
  AND (sqlc.narg('mpn_exact')::text IS NULL OR p.mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category_id')::int IS NULL OR p.category_id IN (SELECT id FROM cat_tree))
  AND (sqlc.narg('package')::text IS NULL OR p.package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::text IS NULL
       OR p.lifecycle_status = ANY(string_to_array(sqlc.narg('lifecycle_status'), ',')::part_lifecycle_status[]))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR p.scope = sqlc.narg('filter_scope'))
  AND (
      sqlc.narg('stock_status')::text IS NULL
      OR ('in_stock'     = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand > 0)
      OR ('low_stock'    = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand >= 0 AND ie.minimum_stock_quantity IS NOT NULL AND ie.quantity_on_hand < ie.minimum_stock_quantity)
      OR ('out_of_stock' = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NOT NULL AND ie.quantity_on_hand = 0)
      OR ('not_tracked'  = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NULL)
  )
  AND (sqlc.narg('part_id_filter')::text IS NULL
       OR p.id = ANY(string_to_array(sqlc.narg('part_id_filter'), ',')::uuid[]))
  AND (sqlc.narg('storage_location_id')::uuid IS NULL
       OR ie.storage_location_id IN (SELECT id FROM loc_tree))
  AND (sqlc.narg('storage_cells')::text IS NULL
       OR array_to_string(ie.storage_coordinate, ',') = ANY(string_to_array(sqlc.narg('storage_cells'), '|')));

-- name: GetParametersByPartIDs :many
-- Returns one resolved row per (part, parameter) pair, preferring user-specified
-- over distributor-sourced values. Used to populate parameter columns in the list view.
SELECT DISTINCT ON (pp.part_id, pp.parameter_id)
    pp.part_id, pd.slug, pp.value, pp.numeric_value, pp.numeric_value_max
FROM part_parameters pp
JOIN parameter_definitions pd ON pd.id = pp.parameter_id
WHERE pp.part_id = ANY(sqlc.arg('part_ids')::uuid[])
  AND pp.parameter_id IS NOT NULL
ORDER BY pp.part_id, pp.parameter_id, (pp.source_type = 'user') DESC, pp.id;

-- name: GetPart :one
SELECT * FROM parts WHERE id = $1;

-- name: GetPartsForManifest :many
-- Batch resolution for note `:part{#uuid}` chips: returns the display fields for
-- each referenced part, scoped to parts the team can see (global or its own) so
-- a note can't resolve another team's private parts.
SELECT id, mpn, manufacturer, lifecycle_status
FROM parts
WHERE id = ANY(sqlc.arg('ids')::uuid[])
  AND (scope = 'global' OR (scope = 'team' AND team_id = sqlc.arg('team_id')));

-- name: GetPartWithInventory :one
-- Like GetPart but LEFT JOINs the team's inventory entry, storage location, and category.
SELECT
    p.id, p.scope, p.team_id, p.mpn, p.manufacturer, p.description,
    p.category, p.category_id, p.category_source, p.package, p.datasheet_url, p.lifecycle_status,
    p.enrichment_source_id, p.enriched_at, p.preferred_parameter_source_id, p.created_at,
    p.short_description, p.product_url, p.image_url,
    c.display_name         AS category_name,
    c.parent_id            AS category_parent_id,
    ie.id                  AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.minimum_stock_quantity,
    ie.reorder_quantity,
    ie.preferred_listing_id,
    ie.storage_location_id,
    ie.storage_coordinate,
    sl.label               AS storage_location_label,
    sl.grid_config         AS storage_location_grid_config,
    sl.parent_id           AS storage_location_parent_id
FROM parts p
LEFT JOIN categories c ON c.id = p.category_id
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE p.id = sqlc.arg('id');

-- name: CreatePart :one
INSERT INTO parts (scope, team_id, mpn, manufacturer, description, category, package,
                   datasheet_url, lifecycle_status, category_id, category_source, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, sqlc.narg('category_id'),
        CASE WHEN sqlc.narg('category_id')::int IS NOT NULL THEN 'user' ELSE 'auto' END,
        sqlc.narg('created_by'))
RETURNING *;

-- name: UpdatePart :one
UPDATE parts
SET description                   = COALESCE(sqlc.narg('description'), description),
    category                      = COALESCE(sqlc.narg('category'), category),
    category_id                   = COALESCE(sqlc.narg('category_id'), category_id),
    category_source               = CASE
                                        WHEN sqlc.narg('category_id')::int IS NOT NULL THEN 'user'
                                        ELSE category_source
                                    END,
    package                       = COALESCE(sqlc.narg('package'), package),
    datasheet_url                 = COALESCE(sqlc.narg('datasheet_url'), datasheet_url),
    lifecycle_status              = COALESCE(sqlc.narg('lifecycle_status'), lifecycle_status),
    preferred_parameter_source_id = COALESCE(sqlc.narg('preferred_parameter_source_id'),
                                             preferred_parameter_source_id)
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListParameters :many
SELECT
    pp.id, pp.part_id, pp.name, pp.value, pp.unit,
    pp.source_type, pp.source_distributor_id,
    pp.parameter_id, pp.numeric_value, pp.numeric_value_max,
    d.name AS source_distributor_name,
    pd.slug AS parameter_slug,
    pd.display_name AS parameter_display_name,
    pd.unit_symbol AS parameter_unit_symbol
FROM part_parameters pp
LEFT JOIN distributors d ON d.id = pp.source_distributor_id
LEFT JOIN parameter_definitions pd ON pd.id = pp.parameter_id
WHERE pp.part_id = $1
ORDER BY pp.source_type, pp.name;

-- name: ResolveUserParameterMapping :one
-- Resolves the canonical parameter definition for a user-specified parameter on a
-- part, so user-entered values (new params and overrides of distributor values)
-- can carry a parameter_id + parsed numeric_value and thus appear in the list-view
-- parameter columns and participate in value search. Preference order:
--   1. Inherit the mapping of an existing distributor-sourced parameter on this
--      part with the same name — the override case (the edit UI reuses the
--      distributor param's raw name, so this matches reliably).
--   2. Otherwise match the registry directly by slug or case-insensitive display
--      name, covering manually-entered parts that have no distributor sibling.
-- Returns no rows when the name maps to no canonical parameter, in which case the
-- caller leaves parameter_id NULL (today's free-text behaviour).
WITH sibling AS (
    SELECT pp.parameter_id
    FROM part_parameters pp
    WHERE pp.part_id = sqlc.arg('part_id')
      AND pp.name = sqlc.arg('name')
      AND pp.source_type = 'distributor'
      AND pp.parameter_id IS NOT NULL
    LIMIT 1
)
SELECT pd.id, pd.value_type
FROM parameter_definitions pd
WHERE pd.id = (SELECT parameter_id FROM sibling)
   OR pd.slug = sqlc.arg('name')
   OR lower(pd.display_name) = lower(sqlc.arg('name'))
ORDER BY (pd.id = (SELECT parameter_id FROM sibling)) DESC NULLS LAST, pd.id
LIMIT 1;

-- name: UpsertUserParameter :one
-- Replaces any existing user-specified parameter with the same name, then inserts.
-- parameter_id / numeric_value(_max) are resolved by the caller (see
-- ResolveUserParameterMapping + paramparse) so a user override participates in the
-- list-view parameter columns and value search exactly like the distributor value
-- it supersedes — honouring the user-wins precedence GetParametersByPartIDs relies on.
WITH deleted AS (
    DELETE FROM part_parameters
    WHERE part_id = $1 AND name = $2 AND source_type = 'user'
)
INSERT INTO part_parameters (part_id, name, value, unit, source_type, parameter_id, numeric_value, numeric_value_max, enum_values)
VALUES ($1, $2, $3, $4, 'user', $5, $6, $7, $8)
RETURNING id, part_id, name, value, unit, source_type, source_distributor_id, parameter_id, numeric_value, numeric_value_max;

-- name: DeleteParameter :exec
-- Only deletes user-specified parameters; distributor-sourced ones are ignored.
-- Bound to part_id so a parameter cannot be deleted via an unrelated part route.
DELETE FROM part_parameters
WHERE id = $1 AND part_id = $2 AND source_type = 'user';

-- name: ListPartParameterValuesByParameterID :many
-- Raw values of every part parameter mapped to a canonical definition. Used to
-- re-parse numeric_value(_max) after the definition's value_type changes or a new
-- mapping links additional rows (admin parameter registry maintenance).
SELECT id, value FROM part_parameters WHERE parameter_id = $1;

-- name: SetPartParameterParsed :exec
-- Overwrites the parsed representation of a single part parameter row (numeric for
-- numeric/range parameters, enum_values for enum_set; the unused ones are NULL).
UPDATE part_parameters SET numeric_value = $2, numeric_value_max = $3, enum_values = $4 WHERE id = $1;

-- name: ListListings :many
SELECT
    dl.id, dl.part_id, dl.distributor_id, d.name AS distributor_name,
    dl.sku, dl.packaging_type, dl.unit_price, dl.stock_available,
    dl.moq, dl.fixed_order_cost, dl.price_breaks, dl.last_refreshed_at
FROM distributor_listings dl
JOIN distributors d ON d.id = dl.distributor_id
WHERE dl.part_id = $1
ORDER BY d.name, dl.packaging_type;

-- name: GetListing :one
SELECT id, part_id, distributor_id, sku FROM distributor_listings WHERE id = $1;

-- name: UpsertListing :one
-- Inserts or updates a distributor listing. On SKU conflict, updates price/stock/moq/price_breaks
-- and distributor category info.
INSERT INTO distributor_listings (part_id, distributor_id, sku, packaging_type,
                                  unit_price, stock_available, moq, fixed_order_cost,
                                  price_breaks, distributor_category_id, distributor_category_name,
                                  last_refreshed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, sqlc.narg('distributor_category_id'),
        sqlc.narg('distributor_category_name'), NOW())
ON CONFLICT (distributor_id, sku) DO UPDATE
SET unit_price                = EXCLUDED.unit_price,
    stock_available           = EXCLUDED.stock_available,
    moq                       = EXCLUDED.moq,
    fixed_order_cost          = EXCLUDED.fixed_order_cost,
    price_breaks              = EXCLUDED.price_breaks,
    distributor_category_id   = COALESCE(EXCLUDED.distributor_category_id, distributor_listings.distributor_category_id),
    distributor_category_name = COALESCE(EXCLUDED.distributor_category_name, distributor_listings.distributor_category_name),
    last_refreshed_at         = NOW()
RETURNING id;

-- name: SetPartEnriched :exec
-- Records which distributor last enriched this part, when, and updates all enrichable
-- fields. NULL keeps existing values (COALESCE pattern).
-- category_id is only updated when the part's category_source is 'auto' and a non-NULL
-- new_category_id is provided; user-assigned categories are never overwritten by enrichment.
UPDATE parts
SET enrichment_source_id = $2,
    enriched_at          = NOW(),
    lifecycle_status     = COALESCE(sqlc.narg('lifecycle_status')::part_lifecycle_status, lifecycle_status),
    mpn                  = COALESCE(sqlc.narg('mpn'), mpn),
    description          = COALESCE(sqlc.narg('description'), description),
    datasheet_url        = COALESCE(sqlc.narg('datasheet_url'), datasheet_url),
    short_description    = COALESCE(sqlc.narg('short_description'), short_description),
    product_url          = COALESCE(sqlc.narg('product_url'), product_url),
    image_url            = COALESCE(sqlc.narg('image_url'), image_url),
    category_id          = CASE
                               WHEN category_source = 'auto' AND sqlc.narg('new_category_id')::int IS NOT NULL
                               THEN sqlc.narg('new_category_id')::int
                               ELSE category_id
                           END
WHERE id = $1;

-- name: UpdateListingData :exec
-- Writes refreshed price/stock/price_breaks data for a single listing.
UPDATE distributor_listings
SET unit_price        = $2,
    stock_available   = $3,
    price_breaks      = $4,
    last_refreshed_at = NOW()
WHERE id = $1;

-- name: GetListingForRefresh :one
-- Fetches the listing plus its distributor's api_type for the worker.
SELECT dl.id, dl.part_id, dl.distributor_id, dl.sku, d.api_type
FROM distributor_listings dl
JOIN distributors d ON d.id = dl.distributor_id
WHERE dl.id = $1;

-- name: GetListingsForRefreshBatch :many
-- Fetches multiple listings by ID in one query for the batch refresh worker.
SELECT dl.id, dl.part_id, dl.distributor_id, dl.sku, d.api_type
FROM distributor_listings dl
JOIN distributors d ON d.id = dl.distributor_id
WHERE dl.id = ANY($1::uuid[]);

-- name: GetRefreshableListingsForPartSearch :many
-- Returns distinct refreshable listing IDs for every part matching the same filter
-- set as ListPartsWithInventory. "Refreshable" = the listing's distributor has an
-- API integration (api_type IS NOT NULL). Used by the parts-table bulk stock refresh
-- so it is scoped to the user's current search/filters rather than a single part.
WITH RECURSIVE cat_tree AS (
  SELECT id FROM categories WHERE id = sqlc.narg('category_id')
  UNION ALL
  SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
),
loc_tree AS (
  SELECT id FROM storage_locations WHERE id = sqlc.narg('storage_location_id')::uuid
  UNION ALL
  SELECT sl.id FROM storage_locations sl JOIN loc_tree lt ON sl.parent_id = lt.id
)
SELECT DISTINCT dl.id AS listing_id
FROM parts p
JOIN distributor_listings dl ON dl.part_id = p.id
JOIN distributors d ON d.id = dl.distributor_id AND d.api_type IS NOT NULL
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
WHERE (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = sqlc.narg('team_id')))
  AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.short_description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%'
       OR EXISTS (SELECT 1 FROM part_aliases pa WHERE pa.part_id = p.id AND pa.alias ILIKE '%' || sqlc.narg('q') || '%'))
  AND (sqlc.narg('mpn_exact')::text IS NULL OR p.mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category_id')::int IS NULL OR p.category_id IN (SELECT id FROM cat_tree))
  AND (sqlc.narg('package')::text IS NULL OR p.package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::text IS NULL
       OR p.lifecycle_status = ANY(string_to_array(sqlc.narg('lifecycle_status'), ',')::part_lifecycle_status[]))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR p.scope = sqlc.narg('filter_scope'))
  AND (
      sqlc.narg('stock_status')::text IS NULL
      OR ('in_stock'     = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand > 0)
      OR ('low_stock'    = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand >= 0 AND ie.minimum_stock_quantity IS NOT NULL AND ie.quantity_on_hand < ie.minimum_stock_quantity)
      OR ('out_of_stock' = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NOT NULL AND ie.quantity_on_hand = 0)
      OR ('not_tracked'  = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NULL)
  )
  AND (sqlc.narg('part_id_filter')::text IS NULL
       OR p.id = ANY(string_to_array(sqlc.narg('part_id_filter'), ',')::uuid[]))
  AND (sqlc.narg('storage_location_id')::uuid IS NULL
       OR ie.storage_location_id IN (SELECT id FROM loc_tree))
  AND (sqlc.narg('storage_cells')::text IS NULL
       OR array_to_string(ie.storage_coordinate, ',') = ANY(string_to_array(sqlc.narg('storage_cells'), '|')));

-- name: GetPartsForReenrichmentInSearch :many
-- Returns one (part_id, sku, distributor_id) per part matching the same filter set
-- as ListPartsWithInventory that has at least one API-integrated distributor listing.
-- Prefers the team's preferred_listing_id, else the most recently created listing.
-- Used by the search-scoped bulk re-enrichment so it is bounded to the user's
-- current search rather than the entire inventory.
WITH RECURSIVE cat_tree AS (
  SELECT id FROM categories WHERE id = sqlc.narg('category_id')
  UNION ALL
  SELECT c.id FROM categories c JOIN cat_tree ct ON c.parent_id = ct.id
),
loc_tree AS (
  SELECT id FROM storage_locations WHERE id = sqlc.narg('storage_location_id')::uuid
  UNION ALL
  SELECT sl.id FROM storage_locations sl JOIN loc_tree lt ON sl.parent_id = lt.id
)
SELECT DISTINCT ON (p.id)
    p.id AS part_id,
    dl.sku,
    dl.distributor_id
FROM parts p
JOIN distributor_listings dl ON dl.part_id = p.id
JOIN distributors d ON d.id = dl.distributor_id AND d.api_type IS NOT NULL
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
WHERE (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = sqlc.narg('team_id')))
  AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.short_description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%'
       OR EXISTS (SELECT 1 FROM part_aliases pa WHERE pa.part_id = p.id AND pa.alias ILIKE '%' || sqlc.narg('q') || '%'))
  AND (sqlc.narg('mpn_exact')::text IS NULL OR p.mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category_id')::int IS NULL OR p.category_id IN (SELECT id FROM cat_tree))
  AND (sqlc.narg('package')::text IS NULL OR p.package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::text IS NULL
       OR p.lifecycle_status = ANY(string_to_array(sqlc.narg('lifecycle_status'), ',')::part_lifecycle_status[]))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR p.scope = sqlc.narg('filter_scope'))
  AND (
      sqlc.narg('stock_status')::text IS NULL
      OR ('in_stock'     = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand > 0)
      OR ('low_stock'    = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.quantity_on_hand >= 0 AND ie.minimum_stock_quantity IS NOT NULL AND ie.quantity_on_hand < ie.minimum_stock_quantity)
      OR ('out_of_stock' = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NOT NULL AND ie.quantity_on_hand = 0)
      OR ('not_tracked'  = ANY(string_to_array(sqlc.narg('stock_status'), ',')) AND ie.id IS NULL)
  )
  AND (sqlc.narg('part_id_filter')::text IS NULL
       OR p.id = ANY(string_to_array(sqlc.narg('part_id_filter'), ',')::uuid[]))
  AND (sqlc.narg('storage_location_id')::uuid IS NULL
       OR ie.storage_location_id IN (SELECT id FROM loc_tree))
  AND (sqlc.narg('storage_cells')::text IS NULL
       OR array_to_string(ie.storage_coordinate, ',') = ANY(string_to_array(sqlc.narg('storage_cells'), '|')))
ORDER BY p.id,
    (ie.preferred_listing_id = dl.id) DESC NULLS LAST,
    dl.id DESC;

-- name: GetListingByPartAndID :one
-- Fetches a listing row verifying it belongs to the given part.
SELECT id, distributor_id, sku FROM distributor_listings
WHERE id = $1 AND part_id = $2;

-- name: DeleteListing :exec
DELETE FROM distributor_listings WHERE id = $1;

-- name: DeleteDistributorParameters :exec
-- Removes all distributor-sourced parameters for a part from one distributor.
-- Called before re-inserting fresh parameters from enrichment.
DELETE FROM part_parameters
WHERE part_id = $1 AND source_type = 'distributor' AND source_distributor_id = $2;

-- name: InsertDistributorParameter :one
INSERT INTO part_parameters (part_id, name, value, unit, source_type, source_distributor_id, parameter_id, numeric_value, numeric_value_max, enum_values)
VALUES ($1, $2, $3, $4, 'distributor', $5, $6, $7, $8, $9)
RETURNING id;

-- name: GetPartByMPN :one
-- Returns the best-matching part for an MPN: team-scoped before global.
-- Used by the BOM import pipeline to resolve raw_description against the catalog.
SELECT id, mpn, manufacturer, description, category, package, lifecycle_status
FROM parts
WHERE mpn = $1
  AND (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND deleted_at IS NULL
ORDER BY scope DESC
LIMIT 1;

-- name: ListPartsByMPN :many
-- Returns all parts matching an MPN (team-scoped before global, up to 10).
-- More than one result indicates an ambiguous MPN within this team's context.
SELECT id, mpn, manufacturer, description, category, package, lifecycle_status
FROM parts
WHERE mpn = $1
  AND (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND deleted_at IS NULL
ORDER BY scope DESC
LIMIT 10;

-- name: ListPartsByMPNAndManufacturer :many
-- Returns parts matching both MPN and manufacturer (case-insensitive ILIKE).
-- Used as the first step in BOM import resolution before falling back to MPN-only.
-- Avoids false ambiguity when the catalog has the same MPN from different manufacturers.
SELECT id, mpn, manufacturer, description, category, package, lifecycle_status
FROM parts
WHERE mpn = $1
  AND manufacturer ILIKE $2
  AND (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND deleted_at IS NULL
ORDER BY scope DESC
LIMIT 10;

-- name: GetPartsForReenrichment :many
-- Returns one (part_id, sku, distributor_id) per part in the team's inventory
-- that has at least one distributor listing. Prefers the preferred_listing_id;
-- falls back to the most-recently-created listing.
SELECT DISTINCT ON (ie.part_id)
    ie.part_id,
    dl.sku,
    dl.distributor_id
FROM inventory_entries ie
JOIN distributor_listings dl ON dl.part_id = ie.part_id
WHERE ie.team_id = @team_id
ORDER BY ie.part_id,
    (ie.preferred_listing_id = dl.id) DESC NULLS LAST,
    dl.id DESC;

-- name: DeletePart :exec
-- Soft-delete: parts are an audit source-of-truth and are never physically
-- removed (that would erase the stock ledger and creation record). Catalog and
-- match queries filter on deleted_at IS NULL.
UPDATE parts SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL;

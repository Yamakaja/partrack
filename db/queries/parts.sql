-- Parts catalog, parameters, and distributor listing queries.

-- name: ListParts :many
SELECT id, scope, team_id, mpn, manufacturer, description, category, package,
       lifecycle_status, created_at
FROM parts
WHERE (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND (sqlc.narg('q')::text IS NULL
       OR mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR description ILIKE '%' || sqlc.narg('q') || '%'
       OR manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('mpn_exact')::text IS NULL OR mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category')::text IS NULL OR category = sqlc.narg('category'))
  AND (sqlc.narg('package')::text IS NULL OR package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::part_lifecycle_status IS NULL
       OR lifecycle_status = sqlc.narg('lifecycle_status'))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR scope = sqlc.narg('filter_scope'))
ORDER BY mpn NULLS LAST, id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountParts :one
SELECT COUNT(*) FROM parts
WHERE (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
  AND (sqlc.narg('q')::text IS NULL
       OR mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR description ILIKE '%' || sqlc.narg('q') || '%'
       OR manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('mpn_exact')::text IS NULL OR mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category')::text IS NULL OR category = sqlc.narg('category'))
  AND (sqlc.narg('package')::text IS NULL OR package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::part_lifecycle_status IS NULL
       OR lifecycle_status = sqlc.narg('lifecycle_status'))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR scope = sqlc.narg('filter_scope'));

-- name: ListPartsWithInventory :many
-- Like ListParts but LEFT JOINs the team's inventory entry for each part.
-- All inventory fields are NULL for parts that are not tracked by the team.
SELECT
    p.id, p.scope, p.team_id, p.mpn, p.manufacturer, p.description,
    p.category, p.package, p.lifecycle_status, p.created_at,
    ie.id            AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.minimum_stock_quantity,
    ie.reorder_quantity,
    sl.label         AS storage_location_label
FROM parts p
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = sqlc.narg('team_id')))
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('mpn_exact')::text IS NULL OR p.mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category')::text IS NULL OR p.category = sqlc.narg('category'))
  AND (sqlc.narg('package')::text IS NULL OR p.package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::part_lifecycle_status IS NULL
       OR p.lifecycle_status = sqlc.narg('lifecycle_status'))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR p.scope = sqlc.narg('filter_scope'))
  AND (
      sqlc.narg('stock_status')::text IS NULL
      OR (sqlc.narg('stock_status')::text = 'in_stock'     AND ie.quantity_on_hand > 0 AND (ie.minimum_stock_quantity IS NULL OR ie.quantity_on_hand >= ie.minimum_stock_quantity))
      OR (sqlc.narg('stock_status')::text = 'low_stock'    AND ie.quantity_on_hand > 0 AND ie.minimum_stock_quantity IS NOT NULL AND ie.quantity_on_hand < ie.minimum_stock_quantity)
      OR (sqlc.narg('stock_status')::text = 'out_of_stock' AND ie.id IS NOT NULL AND ie.quantity_on_hand = 0)
      OR (sqlc.narg('stock_status')::text = 'not_tracked'  AND ie.id IS NULL)
  )
ORDER BY p.mpn NULLS LAST, p.id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountPartsWithInventory :one
SELECT COUNT(*) FROM parts p
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
WHERE (p.scope = 'global' OR (p.scope = 'team' AND p.team_id = sqlc.narg('team_id')))
  AND (sqlc.narg('q')::text IS NULL
       OR p.mpn ILIKE '%' || sqlc.narg('q') || '%'
       OR p.description ILIKE '%' || sqlc.narg('q') || '%'
       OR p.manufacturer ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('mpn_exact')::text IS NULL OR p.mpn = sqlc.narg('mpn_exact'))
  AND (sqlc.narg('category')::text IS NULL OR p.category = sqlc.narg('category'))
  AND (sqlc.narg('package')::text IS NULL OR p.package = sqlc.narg('package'))
  AND (sqlc.narg('lifecycle_status')::part_lifecycle_status IS NULL
       OR p.lifecycle_status = sqlc.narg('lifecycle_status'))
  AND (sqlc.narg('filter_scope')::part_scope IS NULL OR p.scope = sqlc.narg('filter_scope'))
  AND (
      sqlc.narg('stock_status')::text IS NULL
      OR (sqlc.narg('stock_status')::text = 'in_stock'     AND ie.quantity_on_hand > 0 AND (ie.minimum_stock_quantity IS NULL OR ie.quantity_on_hand >= ie.minimum_stock_quantity))
      OR (sqlc.narg('stock_status')::text = 'low_stock'    AND ie.quantity_on_hand > 0 AND ie.minimum_stock_quantity IS NOT NULL AND ie.quantity_on_hand < ie.minimum_stock_quantity)
      OR (sqlc.narg('stock_status')::text = 'out_of_stock' AND ie.id IS NOT NULL AND ie.quantity_on_hand = 0)
      OR (sqlc.narg('stock_status')::text = 'not_tracked'  AND ie.id IS NULL)
  );

-- name: GetPart :one
SELECT id, scope, team_id, mpn, manufacturer, description, category, package,
       datasheet_url, lifecycle_status, enrichment_source_id, enriched_at,
       preferred_parameter_source_id, created_at
FROM parts WHERE id = $1;

-- name: GetPartWithInventory :one
-- Like GetPart but LEFT JOINs the team's inventory entry and storage location.
SELECT
    p.id, p.scope, p.team_id, p.mpn, p.manufacturer, p.description,
    p.category, p.package, p.datasheet_url, p.lifecycle_status,
    p.enrichment_source_id, p.enriched_at, p.preferred_parameter_source_id, p.created_at,
    ie.id                  AS inventory_entry_id,
    ie.quantity_on_hand,
    ie.minimum_stock_quantity,
    ie.reorder_quantity,
    ie.preferred_listing_id,
    ie.storage_location_id,
    sl.label               AS storage_location_label
FROM parts p
LEFT JOIN inventory_entries ie ON ie.part_id = p.id AND ie.team_id = sqlc.narg('team_id')
LEFT JOIN storage_locations sl ON sl.id = ie.storage_location_id
WHERE p.id = sqlc.arg('id');

-- name: CreatePart :one
INSERT INTO parts (scope, team_id, mpn, manufacturer, description, category, package,
                   datasheet_url, lifecycle_status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, scope, team_id, mpn, manufacturer, description, category, package,
          datasheet_url, lifecycle_status, enrichment_source_id, enriched_at,
          preferred_parameter_source_id, created_at;

-- name: UpdatePart :one
UPDATE parts
SET description                   = COALESCE(sqlc.narg('description'), description),
    category                      = COALESCE(sqlc.narg('category'), category),
    package                       = COALESCE(sqlc.narg('package'), package),
    datasheet_url                 = COALESCE(sqlc.narg('datasheet_url'), datasheet_url),
    lifecycle_status              = COALESCE(sqlc.narg('lifecycle_status'), lifecycle_status),
    preferred_parameter_source_id = COALESCE(sqlc.narg('preferred_parameter_source_id'),
                                             preferred_parameter_source_id)
WHERE id = sqlc.arg('id')
RETURNING id, scope, team_id, mpn, manufacturer, description, category, package,
          datasheet_url, lifecycle_status, enrichment_source_id, enriched_at,
          preferred_parameter_source_id, created_at;

-- name: ListParameters :many
SELECT
    pp.id, pp.part_id, pp.name, pp.value, pp.unit,
    pp.source_type, pp.source_distributor_id,
    d.name AS source_distributor_name
FROM part_parameters pp
LEFT JOIN distributors d ON d.id = pp.source_distributor_id
WHERE pp.part_id = $1
ORDER BY pp.source_type, pp.name;

-- name: UpsertUserParameter :one
-- Replaces any existing user-specified parameter with the same name, then inserts.
WITH deleted AS (
    DELETE FROM part_parameters
    WHERE part_id = $1 AND name = $2 AND source_type = 'user'
)
INSERT INTO part_parameters (part_id, name, value, unit, source_type)
VALUES ($1, $2, $3, $4, 'user')
RETURNING id, part_id, name, value, unit, source_type, source_distributor_id;

-- name: DeleteParameter :exec
-- Only deletes user-specified parameters; distributor-sourced ones are ignored.
DELETE FROM part_parameters
WHERE id = $1 AND source_type = 'user';

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
-- Inserts or updates a distributor listing. On SKU conflict, updates price/stock/moq/price_breaks.
INSERT INTO distributor_listings (part_id, distributor_id, sku, packaging_type,
                                  unit_price, stock_available, moq, fixed_order_cost,
                                  price_breaks, last_refreshed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
ON CONFLICT (distributor_id, sku) DO UPDATE
SET unit_price        = EXCLUDED.unit_price,
    stock_available   = EXCLUDED.stock_available,
    moq               = EXCLUDED.moq,
    fixed_order_cost  = EXCLUDED.fixed_order_cost,
    price_breaks      = EXCLUDED.price_breaks,
    last_refreshed_at = NOW()
RETURNING id;

-- name: SetPartEnriched :exec
-- Records which distributor last enriched this part, when, and updates lifecycle_status
-- and mpn if the distributor provides them (NULL keeps the existing values).
UPDATE parts
SET enrichment_source_id = $2,
    enriched_at          = NOW(),
    lifecycle_status     = COALESCE(sqlc.narg('lifecycle_status')::part_lifecycle_status, lifecycle_status),
    mpn                  = COALESCE(sqlc.narg('mpn'), mpn)
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
INSERT INTO part_parameters (part_id, name, value, unit, source_type, source_distributor_id)
VALUES ($1, $2, $3, $4, 'distributor', $5)
RETURNING id;

-- name: GetPartByMPN :one
-- Returns the best-matching part for an MPN: team-scoped before global.
-- Used by the BOM import pipeline to resolve raw_description against the catalog.
SELECT id, mpn, manufacturer, description, category, package, lifecycle_status
FROM parts
WHERE mpn = $1
  AND (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
ORDER BY scope DESC
LIMIT 1;

-- name: ListPartsByMPN :many
-- Returns all parts matching an MPN (team-scoped before global, up to 10).
-- More than one result indicates an ambiguous MPN within this team's context.
SELECT id, mpn, manufacturer, description, category, package, lifecycle_status
FROM parts
WHERE mpn = $1
  AND (scope = 'global' OR (scope = 'team' AND team_id = sqlc.narg('team_id')))
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
ORDER BY scope DESC
LIMIT 10;

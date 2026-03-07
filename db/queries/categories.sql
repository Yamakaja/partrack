-- Category registry and distributor mapping queries.

-- name: ListCategories :many
SELECT id, slug, display_name, parent_id, sort_order, primary_params
FROM categories
ORDER BY sort_order, id;

-- name: GetCategory :one
SELECT id, slug, display_name, parent_id, sort_order, primary_params
FROM categories WHERE id = $1;

-- name: GetCategoryBySlug :one
SELECT id, slug, display_name, parent_id, sort_order, primary_params
FROM categories WHERE slug = $1;

-- name: GetMappedCategory :one
-- Returns the mapping row for a distributor's own category ID.
-- category_id is nullable — NULL means the distributor category is known but unmapped.
SELECT id, distributor_id, distributor_category_id, distributor_category_name, category_id
FROM distributor_category_mappings
WHERE distributor_id = $1 AND distributor_category_id = $2;

-- name: UpsertDistributorCategoryMapping :one
-- When category_id is NULL (enrichment worker registering an unknown category),
-- the existing admin-set mapping is preserved via COALESCE.
-- When category_id is non-NULL (admin setting a mapping), it overwrites the existing value.
INSERT INTO distributor_category_mappings
  (distributor_id, distributor_category_id, distributor_category_name, category_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (distributor_id, distributor_category_id) DO UPDATE
SET distributor_category_name = EXCLUDED.distributor_category_name,
    category_id               = COALESCE(EXCLUDED.category_id, distributor_category_mappings.category_id)
RETURNING id, distributor_id, distributor_category_id, distributor_category_name, category_id;

-- name: ListDistributorCategoryMappings :many
SELECT id, distributor_id, distributor_category_id, distributor_category_name, category_id
FROM distributor_category_mappings
WHERE distributor_id = $1
ORDER BY distributor_category_name;

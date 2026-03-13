-- Index on parts(category_id) to speed up category-filtered queries
-- (GetCategoryParameters, ListPartsWithInventory category CTE filter)
CREATE INDEX IF NOT EXISTS idx_parts_category_id ON parts (category_id) WHERE category_id IS NOT NULL;

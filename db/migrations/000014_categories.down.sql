ALTER TABLE distributor_listings
  DROP COLUMN IF EXISTS distributor_category_id,
  DROP COLUMN IF EXISTS distributor_category_name;

ALTER TABLE parts
  DROP COLUMN IF EXISTS category_source,
  DROP COLUMN IF EXISTS category_id;

DROP TABLE IF EXISTS distributor_category_mappings;
DROP TABLE IF EXISTS categories;

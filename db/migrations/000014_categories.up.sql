-- Partrack category registry
CREATE TABLE categories (
  id             SERIAL PRIMARY KEY,
  slug           TEXT NOT NULL UNIQUE,
  display_name   TEXT NOT NULL,
  parent_id      INT REFERENCES categories(id),
  sort_order     INT NOT NULL DEFAULT 0,
  primary_params JSONB NOT NULL DEFAULT '[]'
);
CREATE INDEX ON categories (parent_id);

-- Distributor → partrack category mapping
CREATE TABLE distributor_category_mappings (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  distributor_id            UUID NOT NULL REFERENCES distributors(id) ON DELETE CASCADE,
  distributor_category_id   TEXT NOT NULL,
  distributor_category_name TEXT NOT NULL DEFAULT '',
  category_id               INT REFERENCES categories(id) ON DELETE SET NULL,
  UNIQUE(distributor_id, distributor_category_id)
);
CREATE INDEX ON distributor_category_mappings (category_id);

-- Add structured category reference to parts and track assignment source
ALTER TABLE parts
  ADD COLUMN category_id     INT REFERENCES categories(id),
  ADD COLUMN category_source TEXT NOT NULL DEFAULT 'auto';
CREATE INDEX ON parts (category_id);

-- Add distributor's own category info to listings (it's a property of the listing, not the part)
ALTER TABLE distributor_listings
  ADD COLUMN distributor_category_id   TEXT,
  ADD COLUMN distributor_category_name TEXT;

-- parts.category (free-text) is kept temporarily for backward compatibility;
-- new code uses category_id. It can be dropped after all parts are re-enriched.

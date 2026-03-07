ALTER TABLE parts
  ADD COLUMN short_description TEXT,
  ADD COLUMN product_url TEXT,
  ADD COLUMN image_url TEXT;

CREATE TABLE part_aliases (
  id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  part_id UUID NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  alias   TEXT NOT NULL,
  UNIQUE(part_id, alias)
);
CREATE INDEX ON part_aliases (part_id);

-- Generic/parametric BOM part support (see doc/bom-import.md).

-- Per-category class profile: the parameter slug that the bare value field
-- encodes (the "magnitude"). NULL => the category has no parametric magnitude,
-- so the value is treated as an identity/text reference rather than parsed.
ALTER TABLE categories ADD COLUMN value_param TEXT;

-- Normalised package identifier, compared on BOTH distributor enrichment and
-- generic-part creation so a generic "0603" matches an enriched
-- "0603 (1608 Metric)". Populated by the genspec package normaliser.
ALTER TABLE parts ADD COLUMN canonical_package TEXT;
CREATE INDEX ON parts (canonical_package) WHERE canonical_package IS NOT NULL;

-- BOM line item: the raw parametric inputs and the persisted, user-reviewable
-- interpretation produced by the genspec parser. interpretation_source tracks
-- whether a human edited it, so re-parses never clobber user corrections.
ALTER TABLE bom_line_items
  ADD COLUMN value                 TEXT,
  ADD COLUMN footprint             TEXT,
  ADD COLUMN interpretation        JSONB,
  ADD COLUMN interpretation_source TEXT NOT NULL DEFAULT 'auto'
    CHECK (interpretation_source IN ('auto', 'user'));

-- Canonical parameter emitted by the parser for LED colour.
INSERT INTO parameter_definitions (slug, display_name, unit, unit_symbol, value_type)
VALUES ('color', 'Color', NULL, NULL, 'enum')
ON CONFLICT (slug) DO NOTHING;

-- Seed class profiles for the parametric (passive) categories. value_param names
-- the primary magnitude; primary_params lists the parameters that matter and
-- gate the parser's secondary recognizers. All other categories stay
-- identity-only (value_param NULL).
UPDATE categories SET value_param = 'resistance',
  primary_params = '["resistance","tolerance","power_rating","package"]'
  WHERE slug = 'resistors';
UPDATE categories SET value_param = 'capacitance',
  primary_params = '["capacitance","voltage_rating","dielectric","tolerance","package"]'
  WHERE slug = 'capacitors';
UPDATE categories SET value_param = 'inductance',
  primary_params = '["inductance","tolerance","current_rating","package"]'
  WHERE slug = 'inductors-coils-chokes';
UPDATE categories SET value_param = 'frequency',
  primary_params = '["frequency","tolerance","package"]'
  WHERE slug = 'crystals-oscillators-resonators';

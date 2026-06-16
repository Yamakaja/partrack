UPDATE categories SET value_param = NULL, primary_params = '[]'
  WHERE slug IN ('resistors', 'capacitors', 'inductors-coils-chokes',
                 'crystals-oscillators-resonators');

DELETE FROM parameter_definitions WHERE slug = 'color';

ALTER TABLE bom_line_items
  DROP COLUMN interpretation_source,
  DROP COLUMN interpretation,
  DROP COLUMN footprint,
  DROP COLUMN value;

ALTER TABLE parts DROP COLUMN canonical_package;

ALTER TABLE categories DROP COLUMN value_param;

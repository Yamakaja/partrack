DROP INDEX IF EXISTS part_parameters_parameter_id_numeric_value_idx;
ALTER TABLE part_parameters
  DROP COLUMN IF EXISTS numeric_value_max,
  DROP COLUMN IF EXISTS numeric_value,
  DROP COLUMN IF EXISTS parameter_id;

DROP INDEX IF EXISTS distributor_parameter_mappings_parameter_id_idx;
DROP TABLE IF EXISTS distributor_parameter_mappings;
DROP TABLE IF EXISTS parameter_definitions;

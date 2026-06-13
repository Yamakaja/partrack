DROP INDEX IF EXISTS part_parameters_enum_values_idx;

ALTER TABLE part_parameters DROP COLUMN IF EXISTS enum_values;

ALTER TABLE parameter_definitions
  DROP CONSTRAINT parameter_definitions_value_type_check,
  ADD CONSTRAINT parameter_definitions_value_type_check
    CHECK (value_type IN ('numeric', 'range', 'text', 'enum'));

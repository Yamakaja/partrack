-- Add the 'enum_set' parameter kind: a parameter whose value is a list of enum
-- members (e.g. "CANbus, EBI/EMI, Ethernet"). The parsed members are stored in a
-- text[] sidecar on part_parameters (mirroring how numeric_value sidecars value),
-- GIN-indexed for contains-any/all search via the && / @> operators.

ALTER TABLE parameter_definitions
  DROP CONSTRAINT parameter_definitions_value_type_check,
  ADD CONSTRAINT parameter_definitions_value_type_check
    CHECK (value_type IN ('numeric', 'range', 'text', 'enum', 'enum_set'));

ALTER TABLE part_parameters ADD COLUMN enum_values text[];

CREATE INDEX part_parameters_enum_values_idx
  ON part_parameters USING GIN (enum_values)
  WHERE enum_values IS NOT NULL;

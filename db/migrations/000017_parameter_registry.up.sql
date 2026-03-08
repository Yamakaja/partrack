-- Partrack parameter registry: canonical parameter definitions
CREATE TABLE parameter_definitions (
  id           SERIAL PRIMARY KEY,
  slug         TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  unit         TEXT,         -- base SI unit identifier (null for dimensionless/text params)
  unit_symbol  TEXT,         -- display symbol (e.g. Ω, F, V); may differ from unit identifier
  value_type   TEXT NOT NULL DEFAULT 'numeric'
               CHECK (value_type IN ('numeric', 'range', 'text', 'enum'))
);

-- Maps distributor-specific parameter identifiers to partrack parameters.
-- Rows with parameter_id IS NULL represent unmapped params awaiting admin review.
CREATE TABLE distributor_parameter_mappings (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  distributor_id         UUID NOT NULL REFERENCES distributors(id) ON DELETE CASCADE,
  distributor_param_id   TEXT NOT NULL,           -- distributor's own stable parameter ID
  distributor_param_name TEXT NOT NULL DEFAULT '', -- distributor's display name (for reference)
  parameter_id           INT REFERENCES parameter_definitions(id) ON DELETE SET NULL,
  ignored                BOOLEAN NOT NULL DEFAULT FALSE,
  UNIQUE(distributor_id, distributor_param_id)
);
CREATE INDEX ON distributor_parameter_mappings (parameter_id);

-- Extend part_parameters with structured/parsed data.
-- parameter_id is nullable: legacy rows (pre-registry) remain valid.
ALTER TABLE part_parameters
  ADD COLUMN parameter_id      INT REFERENCES parameter_definitions(id) ON DELETE SET NULL,
  ADD COLUMN numeric_value     DOUBLE PRECISION,     -- parsed value in base SI units
  ADD COLUMN numeric_value_max DOUBLE PRECISION;     -- for range params: numeric_value is min, this is max

CREATE INDEX ON part_parameters (parameter_id, numeric_value)
  WHERE numeric_value IS NOT NULL;

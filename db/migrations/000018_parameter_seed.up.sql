-- Seed: canonical partrack parameter definitions.
-- These are the well-known parameters used across common electronic component types.
-- DigiKey parameter ID mappings are NOT included here because DigiKey's ParameterIds
-- must be verified from live API responses. Once verified, add a follow-up migration
-- (000019_parameter_mappings_digikey.up.sql) with the actual IDs.
-- In the meantime, the admin UI (/admin/parameters) shows all unmapped distributor
-- parameters discovered during enrichment, and admins can link them manually.

INSERT INTO parameter_definitions (id, slug, display_name, unit, unit_symbol, value_type) VALUES
  (1,  'resistance',        'Resistance',         'ohm',    'Ω',  'numeric'),
  (2,  'capacitance',       'Capacitance',        'farad',  'F',  'numeric'),
  (3,  'inductance',        'Inductance',         'henry',  'H',  'numeric'),
  (4,  'voltage_rating',    'Voltage Rating',     'volt',   'V',  'numeric'),
  (5,  'current_rating',    'Current Rating',     'ampere', 'A',  'numeric'),
  (6,  'power_rating',      'Power Rating',       'watt',   'W',  'numeric'),
  (7,  'tolerance',         'Tolerance',          '%',      '%',  'numeric'),
  (8,  'frequency',         'Frequency',          'hertz',  'Hz', 'numeric'),
  (9,  'temperature_range', 'Operating Temp.',    '°C',     '°C', 'range'),
  (10, 'package',           'Package / Case',     NULL,     NULL, 'text'),
  (11, 'mounting_type',     'Mounting Type',      NULL,     NULL, 'enum'),
  (12, 'dc_resistance',     'DC Resistance',      'ohm',    'Ω',  'numeric'),
  (13, 'voltage_forward',   'Forward Voltage',    'volt',   'V',  'numeric'),
  (14, 'current_forward',   'Forward Current',    'ampere', 'A',  'numeric'),
  (15, 'breakdown_voltage', 'Breakdown Voltage',  'volt',   'V',  'numeric'),
  (16, 'gain_bandwidth',    'Gain Bandwidth',     'hertz',  'Hz', 'numeric'),
  (17, 'supply_voltage',    'Supply Voltage',     'volt',   'V',  'numeric'),
  (18, 'output_current',    'Output Current',     'ampere', 'A',  'numeric'),
  (19, 'switching_freq',    'Switching Frequency','hertz',  'Hz', 'numeric'),
  (20, 'dielectric',        'Dielectric',         NULL,     NULL, 'enum');

-- Sync the sequence so future INSERTs don't collide with the seeded IDs.
SELECT setval('parameter_definitions_id_seq', (SELECT MAX(id) FROM parameter_definitions));

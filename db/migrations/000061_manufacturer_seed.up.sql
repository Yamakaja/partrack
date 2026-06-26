-- Seed: well-known manufacturer name equivalences.
--
-- Conservative on purpose — only clear spelling/acronym synonyms for the same legal
-- entity (TI ≡ Texas Instruments, ST ≡ STMicroelectronics, …). Acquisition mergers
-- (e.g. Linear Technology → Analog Devices) are intentionally NOT seeded; those are
-- judgement calls left to admin curation / learned proposals (Phase 2).
--
-- normalized_name / alias_normalized values must equal mfrmatch.Normalize(name):
-- lowercased, non-alphanumerics → spaces, whitespace collapsed, trailing corporate
-- suffixes (Inc, Ltd, …) stripped. Each manufacturer's canonical spelling is also
-- inserted as an alias so the resolver maps the canonical name to itself.
--
-- Like parameter_definitions, this is reference data: NOT truncated between tests.

INSERT INTO manufacturers (canonical_name, normalized_name) VALUES
    ('Texas Instruments',      'texas instruments'),
    ('STMicroelectronics',     'stmicroelectronics'),
    ('Analog Devices',         'analog devices'),
    ('onsemi',                 'onsemi'),
    ('NXP Semiconductors',     'nxp semiconductors'),
    ('Infineon Technologies',  'infineon technologies'),
    ('Microchip Technology',   'microchip technology'),
    ('Vishay Intertechnology', 'vishay intertechnology'),
    ('Renesas Electronics',    'renesas electronics'),
    ('Murata Manufacturing',   'murata manufacturing');

INSERT INTO manufacturer_aliases (manufacturer_id, alias, alias_normalized, source)
SELECT m.id, v.alias, v.alias_normalized, 'seed'
FROM (VALUES
    ('texas instruments',      'Texas Instruments',      'texas instruments'),
    ('texas instruments',      'TI',                     'ti'),
    ('stmicroelectronics',     'STMicroelectronics',     'stmicroelectronics'),
    ('stmicroelectronics',     'ST',                     'st'),
    ('stmicroelectronics',     'ST Microelectronics',    'st microelectronics'),
    ('stmicroelectronics',     'STMicro',                'stmicro'),
    ('analog devices',         'Analog Devices',         'analog devices'),
    ('analog devices',         'ADI',                    'adi'),
    ('onsemi',                 'onsemi',                 'onsemi'),
    ('onsemi',                 'ON Semiconductor',       'on semiconductor'),
    ('onsemi',                 'ON Semi',                'on semi'),
    ('nxp semiconductors',     'NXP Semiconductors',     'nxp semiconductors'),
    ('nxp semiconductors',     'NXP',                    'nxp'),
    ('infineon technologies',  'Infineon Technologies',  'infineon technologies'),
    ('infineon technologies',  'Infineon',               'infineon'),
    ('microchip technology',   'Microchip Technology',   'microchip technology'),
    ('microchip technology',   'Microchip',              'microchip'),
    ('vishay intertechnology', 'Vishay Intertechnology', 'vishay intertechnology'),
    ('vishay intertechnology', 'Vishay',                 'vishay'),
    ('renesas electronics',    'Renesas Electronics',    'renesas electronics'),
    ('renesas electronics',    'Renesas',                'renesas'),
    ('murata manufacturing',   'Murata Manufacturing',   'murata manufacturing'),
    ('murata manufacturing',   'Murata',                 'murata'),
    ('murata manufacturing',   'Murata Electronics',     'murata electronics')
) AS v(mfr_norm, alias, alias_normalized)
JOIN manufacturers m ON m.normalized_name = v.mfr_norm;

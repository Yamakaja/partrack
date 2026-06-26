-- Removes the seeded manufacturers (aliases cascade via FK). Admin-added rows with
-- different normalized_names are left untouched.
DELETE FROM manufacturers WHERE normalized_name IN (
    'texas instruments', 'stmicroelectronics', 'analog devices', 'onsemi',
    'nxp semiconductors', 'infineon technologies', 'microchip technology',
    'vishay intertechnology', 'renesas electronics', 'murata manufacturing'
);

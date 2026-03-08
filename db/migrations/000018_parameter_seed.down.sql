DELETE FROM parameter_definitions WHERE id BETWEEN 1 AND 20;
SELECT setval('parameter_definitions_id_seq', 1, false);

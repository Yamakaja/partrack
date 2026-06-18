-- generic_code marks a category as supporting synthesized generic parts and
-- supplies the short type code used in the generated MPN (e.g. GEN-RES-10K-0402).
-- NULL means the category does not support generic-part identity synthesis.
ALTER TABLE categories ADD COLUMN generic_code text;

-- Seed the commodity categories generic parts are actually created for. More can be
-- enabled later by setting generic_code on the relevant category.
UPDATE categories SET generic_code = 'RES' WHERE slug = 'resistors';
UPDATE categories SET generic_code = 'CAP' WHERE slug = 'capacitors';
UPDATE categories SET generic_code = 'IND' WHERE slug = 'inductors-coils-chokes';

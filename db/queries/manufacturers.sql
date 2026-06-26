-- name: ListManufacturerAliases :many
-- Loads the full alias → canonical-manufacturer map for the in-memory resolver
-- cache (internal/mfrmatch). Bounded reference data; read once at startup and
-- again after admin mutations.
SELECT a.alias_normalized,
       a.manufacturer_id,
       m.canonical_name,
       m.normalized_name
FROM manufacturer_aliases a
JOIN manufacturers m ON m.id = a.manufacturer_id;

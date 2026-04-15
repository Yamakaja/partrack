ALTER TABLE storage_locations
    ADD COLUMN parent_id UUID REFERENCES storage_locations(id) ON DELETE RESTRICT,
    ADD COLUMN grid_config JSONB;

-- Replace team-wide label uniqueness with sibling-scoped uniqueness.
-- NULLS NOT DISTINCT ensures root nodes (parent_id IS NULL) are also deduplicated per team.
ALTER TABLE storage_locations
    DROP CONSTRAINT storage_locations_team_id_label_key;

CREATE UNIQUE INDEX storage_locations_sibling_label_unique
    ON storage_locations (team_id, parent_id, label) NULLS NOT DISTINCT;

ALTER TABLE inventory_entries
    ADD COLUMN storage_coordinate INTEGER[];

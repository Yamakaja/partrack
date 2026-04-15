ALTER TABLE inventory_entries
    DROP COLUMN IF EXISTS storage_coordinate;

DROP INDEX IF EXISTS storage_locations_sibling_label_unique;

ALTER TABLE storage_locations
    ADD CONSTRAINT storage_locations_team_id_label_key UNIQUE (team_id, label);

ALTER TABLE storage_locations
    DROP COLUMN IF EXISTS grid_config,
    DROP COLUMN IF EXISTS parent_id;

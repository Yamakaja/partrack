ALTER TABLE builds
    ADD COLUMN excluded_designators TEXT[] NOT NULL DEFAULT '{}';

CREATE TABLE build_line_item_overrides (
    build_id          UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    bom_line_item_id  UUID NOT NULL REFERENCES bom_line_items(id) ON DELETE CASCADE,
    override_type     TEXT NOT NULL CHECK (override_type IN ('use_available', 'skip')),
    PRIMARY KEY (build_id, bom_line_item_id)
);

CREATE INDEX ON build_line_item_overrides (build_id);

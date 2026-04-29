CREATE TABLE part_resolve_items (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id          UUID        NOT NULL REFERENCES teams(id),
    source_type      TEXT        NOT NULL DEFAULT 'manual',
    source_id        UUID,
    status           TEXT        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending','enriched','committed','failed')),
    task_type        TEXT        CHECK (task_type IN (
                                     'unknown_part_import',
                                     'known_part_create_entry',
                                     'known_part_stock_adjustment',
                                     'unrecognized')),
    input_fields     JSONB       NOT NULL,
    enrichment_data  JSONB,
    resolved_part_id UUID        REFERENCES parts(id),
    error_message    TEXT,
    created_by       UUID        NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    enriched_at      TIMESTAMPTZ,
    committed_at     TIMESTAMPTZ
);

CREATE INDEX part_resolve_items_team_status
    ON part_resolve_items(team_id, status);
CREATE INDEX part_resolve_items_source
    ON part_resolve_items(source_type, source_id)
    WHERE source_id IS NOT NULL;

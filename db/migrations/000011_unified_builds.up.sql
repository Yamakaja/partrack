-- =============================================================
-- Phase 1a: Extend builds table
-- =============================================================

-- Drop NOT NULL / default on columns being repurposed
ALTER TABLE builds ALTER COLUMN recorded_by DROP NOT NULL;
ALTER TABLE builds ALTER COLUMN built_at   DROP NOT NULL;
ALTER TABLE builds ALTER COLUMN built_at   DROP DEFAULT;

-- Rename legacy columns
ALTER TABLE builds RENAME COLUMN boards_built TO quantity;
ALTER TABLE builds RENAME COLUMN built_at     TO assembled_at;
ALTER TABLE builds RENAME COLUMN recorded_by  TO assembled_by;

-- Add lifecycle columns
ALTER TABLE builds ADD COLUMN status TEXT NOT NULL DEFAULT 'assembled'
    CHECK (status IN ('draft','pending','ordered','ready','assembled','cancelled'));
ALTER TABLE builds ADD COLUMN assembled_quantity INT CHECK (assembled_quantity > 0);
ALTER TABLE builds ADD COLUMN created_at TIMESTAMPTZ;
ALTER TABLE builds ADD COLUMN created_by UUID REFERENCES users(id);
ALTER TABLE builds ADD COLUMN excluded_from_cart BOOLEAN NOT NULL DEFAULT FALSE;

-- Back-fill created_at / created_by from legacy assembled rows
UPDATE builds SET created_at = assembled_at, created_by = assembled_by;
ALTER TABLE builds ALTER COLUMN created_at SET NOT NULL;

-- Index for pipeline queries
CREATE INDEX ON builds (team_id, status);

-- =============================================================
-- Phase 1b: Migrate cart_pending_builds → builds, then drop
-- =============================================================

INSERT INTO builds (team_id, project_id, bom_revision_id, quantity, status, created_at)
SELECT sc.team_id, br.project_id, cpb.bom_revision_id, cpb.build_quantity, 'pending', NOW()
FROM   cart_pending_builds cpb
JOIN   shopping_carts sc ON sc.id = cpb.shopping_cart_id
JOIN   bom_revisions  br ON br.id = cpb.bom_revision_id;

DROP TABLE cart_pending_builds;

-- =============================================================
-- Phase 1c: New tables
-- =============================================================

CREATE TABLE build_orders (
    build_id UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    PRIMARY KEY (build_id, order_id)
);
CREATE INDEX ON build_orders (order_id);

CREATE TABLE build_part_allocations (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    build_id          UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    part_id           UUID NOT NULL REFERENCES parts(id),
    quantity_needed   INT  NOT NULL CHECK (quantity_needed > 0),
    quantity_reserved INT  NOT NULL DEFAULT 0 CHECK (quantity_reserved >= 0),
    quantity_received INT  NOT NULL DEFAULT 0 CHECK (quantity_received >= 0),
    UNIQUE (build_id, part_id)
);
CREATE INDEX ON build_part_allocations (part_id);

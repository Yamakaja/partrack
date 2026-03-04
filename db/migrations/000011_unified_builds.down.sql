-- Drop new tables
DROP TABLE IF EXISTS build_part_allocations;
DROP TABLE IF EXISTS build_orders;

-- Recreate cart_pending_builds (empty)
CREATE TABLE cart_pending_builds (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shopping_cart_id UUID NOT NULL REFERENCES shopping_carts(id) ON DELETE CASCADE,
    bom_revision_id  UUID NOT NULL REFERENCES bom_revisions(id),
    build_quantity   INT  NOT NULL CHECK (build_quantity > 0),
    UNIQUE (shopping_cart_id, bom_revision_id)
);
CREATE INDEX ON cart_pending_builds (shopping_cart_id);

-- Remove rows that were migrated from cart_pending_builds (those with status='pending' and no assembled_at)
DELETE FROM builds WHERE status = 'pending' AND assembled_at IS NULL AND assembled_by IS NULL;

-- Drop new columns
ALTER TABLE builds DROP COLUMN IF EXISTS excluded_from_cart;
ALTER TABLE builds DROP COLUMN IF EXISTS created_by;
ALTER TABLE builds DROP COLUMN IF EXISTS created_at;
ALTER TABLE builds DROP COLUMN IF EXISTS assembled_quantity;
ALTER TABLE builds DROP COLUMN IF EXISTS status;

-- Drop new index
DROP INDEX IF EXISTS builds_team_id_status_idx;

-- Rename columns back
ALTER TABLE builds RENAME COLUMN quantity        TO boards_built;
ALTER TABLE builds RENAME COLUMN assembled_at    TO built_at;
ALTER TABLE builds RENAME COLUMN assembled_by    TO recorded_by;

-- Restore NOT NULL and DEFAULT on built_at and recorded_by
ALTER TABLE builds ALTER COLUMN built_at   SET NOT NULL;
ALTER TABLE builds ALTER COLUMN built_at   SET DEFAULT NOW();
ALTER TABLE builds ALTER COLUMN recorded_by SET NOT NULL;

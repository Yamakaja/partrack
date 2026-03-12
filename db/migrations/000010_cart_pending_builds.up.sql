ALTER TABLE cart_line_items
    DROP CONSTRAINT bom_shortfall_requires_line_item;

CREATE TABLE cart_pending_builds (
    id               UUID PRIMARY KEY DEFAULT uuidv7(),
    shopping_cart_id UUID NOT NULL REFERENCES shopping_carts(id) ON DELETE CASCADE,
    bom_revision_id  UUID NOT NULL REFERENCES bom_revisions(id),
    build_quantity   INT  NOT NULL CHECK (build_quantity > 0),
    UNIQUE (shopping_cart_id, bom_revision_id)
);
CREATE INDEX ON cart_pending_builds (shopping_cart_id);

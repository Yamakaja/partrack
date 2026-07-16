-- Part collections ("Lists"): user-private bags of parts with optional quantities.
-- A collection is a passive container — all semantics live in the operations applied
-- to it (add-to-cart, checkout, scanner filter/collect). See doc/datamodel.md.

CREATE TABLE part_collections (
    id                  UUID        PRIMARY KEY DEFAULT uuidv7(),
    user_id             UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id             UUID        NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,
    archived            BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Scanner Collect quantity-capture toggle. NULL = not asked yet (first collect
    -- scan into this list prompts and persists the answer).
    collect_capture_qty BOOLEAN,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Owner's panel queries: my lists in a team, recently-used first.
CREATE INDEX idx_part_collections_owner
    ON part_collections (user_id, team_id, archived, updated_at DESC);

CREATE TABLE part_collection_items (
    -- uuidv7 PK doubles as the insertion-order sort key.
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    collection_id UUID NOT NULL REFERENCES part_collections(id) ON DELETE CASCADE,
    part_id       UUID NOT NULL REFERENCES parts(id),
    -- NULL = bare membership (no quantity recorded). Quantitative operations skip
    -- and report such rows. 0 is not a state — delete the row instead.
    quantity      INT CHECK (quantity IS NULL OR quantity > 0),
    note          TEXT,
    UNIQUE (collection_id, part_id)
);

CREATE EXTENSION IF NOT EXISTS pg_uuidv7;

-- =============================================================
-- ENUM TYPES
-- =============================================================

CREATE TYPE team_role AS ENUM (
    'viewer', 'member', 'purchaser', 'admin'
);

CREATE TYPE part_scope AS ENUM (
    'global', 'team'
);

CREATE TYPE part_lifecycle_status AS ENUM (
    'active', 'nrnd', 'obsolete', 'deprecated', 'no_longer_available'
);

CREATE TYPE parameter_source_type AS ENUM (
    'user', 'distributor'
);

CREATE TYPE project_status AS ENUM (
    'active', 'archived', 'completed'
);

CREATE TYPE order_status AS ENUM (
    'draft', 'exported', 'submitted', 'placed',
    'partially_received', 'received', 'cancelled'
);

CREATE TYPE order_submission_method AS ENUM (
    'api', 'manual'
);

CREATE TYPE cart_item_source AS ENUM (
    'bom_shortfall', 'inventory_maintenance', 'manual'
);


-- =============================================================
-- IDENTITY & AUTH
-- =============================================================

CREATE TABLE users (
    id           UUID        PRIMARY KEY DEFAULT uuidv7(),
    display_name TEXT        NOT NULL,
    email        TEXT        NOT NULL UNIQUE,
    is_active    BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE teams (
    id          UUID        PRIMARY KEY DEFAULT uuidv7(),
    name        TEXT        NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE team_memberships (
    id         UUID        PRIMARY KEY DEFAULT uuidv7(),
    team_id    UUID        NOT NULL REFERENCES teams(id),
    user_id    UUID        NOT NULL REFERENCES users(id),
    role       team_role   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (team_id, user_id)
);

CREATE TABLE oidc_identities (
    id             UUID        PRIMARY KEY DEFAULT uuidv7(),
    user_id        UUID        NOT NULL REFERENCES users(id),
    provider       TEXT        NOT NULL,  -- matches config-file provider slug
    subject        TEXT        NOT NULL,  -- 'sub' claim from ID token
    provider_email TEXT,                  -- informational; user.email is authoritative
    linked_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, subject)
);

CREATE TABLE user_credentials (
    id            UUID        PRIMARY KEY DEFAULT uuidv7(),
    user_id       UUID        NOT NULL UNIQUE REFERENCES users(id),
    password_hash TEXT        NOT NULL,  -- bcrypt
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Opaque JSON blob for frontend configuration (theme, locale, UI state, etc.)
-- Row is created lazily on first PUT; absence means empty preferences ({}).
CREATE TABLE user_preferences (
    user_id     UUID        PRIMARY KEY REFERENCES users(id),
    preferences JSONB       NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE sessions (
    id           UUID        PRIMARY KEY DEFAULT uuidv7(),
    user_id      UUID        NOT NULL REFERENCES users(id),
    token_hash   TEXT        NOT NULL UNIQUE,  -- SHA-256 hex of raw session token
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_agent   TEXT,
    ip_address   TEXT
);
CREATE INDEX ON sessions (user_id);

CREATE TABLE api_tokens (
    id           UUID        PRIMARY KEY DEFAULT uuidv7(),
    user_id      UUID        NOT NULL REFERENCES users(id),
    name         TEXT        NOT NULL,
    token_hash   TEXT        NOT NULL UNIQUE,  -- SHA-256 hex of raw token
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ
);
CREATE INDEX ON api_tokens (user_id);


-- =============================================================
-- DISTRIBUTORS
-- =============================================================

CREATE TABLE distributors (
    id       UUID PRIMARY KEY DEFAULT uuidv7(),
    name     TEXT NOT NULL,
    website  TEXT,
    api_type TEXT  -- integration type identifier; NULL if no API integration
);

CREATE TABLE distributor_configs (
    id                     UUID        PRIMARY KEY DEFAULT uuidv7(),
    team_id                UUID        NOT NULL REFERENCES teams(id),
    distributor_id         UUID        NOT NULL REFERENCES distributors(id),
    enabled                BOOLEAN     NOT NULL DEFAULT FALSE,
    preferred              BOOLEAN     NOT NULL DEFAULT FALSE,
    cache_duration_seconds INT         NOT NULL DEFAULT 3600,
    api_credentials        JSONB,      -- plaintext; NULL if no API integration
    UNIQUE (team_id, distributor_id)
);


-- =============================================================
-- PARTS
-- =============================================================

CREATE TABLE parts (
    id                            UUID                  PRIMARY KEY DEFAULT uuidv7(),
    scope                         part_scope            NOT NULL,
    team_id                       UUID                  REFERENCES teams(id),  -- NULL for global parts
    mpn                           TEXT,
    manufacturer                  TEXT,
    description                   TEXT,
    category                      TEXT,
    package                       TEXT,
    datasheet_url                 TEXT,
    lifecycle_status              part_lifecycle_status NOT NULL DEFAULT 'active',
    enrichment_source_id          UUID                  REFERENCES distributors(id),
    enriched_at                   TIMESTAMPTZ,
    preferred_parameter_source_id UUID                  REFERENCES distributors(id),
    created_at                    TIMESTAMPTZ           NOT NULL DEFAULT NOW(),
    CONSTRAINT part_scope_team_consistency CHECK (
        (scope = 'global' AND team_id IS NULL) OR
        (scope = 'team'   AND team_id IS NOT NULL)
    )
);
CREATE INDEX ON parts (mpn)     WHERE mpn IS NOT NULL;
CREATE INDEX ON parts (team_id) WHERE team_id IS NOT NULL;

CREATE TABLE part_parameters (
    id                    UUID                  PRIMARY KEY DEFAULT uuidv7(),
    part_id               UUID                  NOT NULL REFERENCES parts(id),
    name                  TEXT                  NOT NULL,  -- normalised key e.g. 'resistance'
    value                 TEXT                  NOT NULL,
    unit                  TEXT,
    source_type           parameter_source_type NOT NULL,
    source_distributor_id UUID                  REFERENCES distributors(id),
    CONSTRAINT distributor_source_requires_distributor_id CHECK (
        source_type != 'distributor' OR source_distributor_id IS NOT NULL
    )
);
CREATE INDEX ON part_parameters (part_id);

CREATE TABLE distributor_listings (
    id                UUID           PRIMARY KEY DEFAULT uuidv7(),
    part_id           UUID           NOT NULL REFERENCES parts(id),
    distributor_id    UUID           NOT NULL REFERENCES distributors(id),
    sku               TEXT           NOT NULL,
    packaging_type    TEXT           NOT NULL,
    unit_price        NUMERIC(14, 4),   -- cached; NULL before first refresh
    stock_available   INT,              -- cached; NULL before first refresh
    moq               INT            NOT NULL DEFAULT 1 CHECK (moq > 0),
    fixed_order_cost  NUMERIC(14, 4) NOT NULL DEFAULT 0,
    last_refreshed_at TIMESTAMPTZ,
    UNIQUE (distributor_id, sku)
);
CREATE INDEX ON distributor_listings (part_id);


-- =============================================================
-- INVENTORY
-- =============================================================

CREATE TABLE storage_locations (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    team_id     UUID NOT NULL REFERENCES teams(id),
    label       TEXT NOT NULL,
    description TEXT,
    UNIQUE (team_id, label)
);

CREATE TABLE inventory_entries (
    id                     UUID    PRIMARY KEY DEFAULT uuidv7(),
    team_id                UUID    NOT NULL REFERENCES teams(id),
    part_id                UUID    NOT NULL REFERENCES parts(id),
    quantity_on_hand       INT     NOT NULL DEFAULT 0,
    storage_location_id    UUID    REFERENCES storage_locations(id),
    preferred_listing_id   UUID    REFERENCES distributor_listings(id),
    minimum_stock_quantity INT     CHECK (minimum_stock_quantity >= 0),
    reorder_quantity       INT     CHECK (reorder_quantity > 0),
    UNIQUE (team_id, part_id)
);
CREATE INDEX ON inventory_entries (team_id);

-- stock_adjustments source FKs are added via ALTER TABLE after
-- builds, general_checkouts, and order_line_items are defined below.
CREATE TABLE stock_adjustments (
    id                  UUID        PRIMARY KEY DEFAULT uuidv7(),
    inventory_entry_id  UUID        NOT NULL REFERENCES inventory_entries(id),
    delta               INT         NOT NULL,
    adjusted_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason              TEXT,
    created_by          UUID        NOT NULL REFERENCES users(id),
    order_line_item_id  UUID,  -- FK constraint added at end of file
    build_id            UUID,  -- FK constraint added at end of file
    general_checkout_id UUID,  -- FK constraint added at end of file
    -- At most one source reference may be set
    CONSTRAINT stock_adjustment_single_source CHECK (
        (order_line_item_id  IS NOT NULL)::int +
        (build_id            IS NOT NULL)::int +
        (general_checkout_id IS NOT NULL)::int <= 1
    ),
    -- Manual adjustments (all sources NULL) must supply a reason
    CONSTRAINT manual_adjustment_requires_reason CHECK (
        order_line_item_id  IS NOT NULL OR
        build_id            IS NOT NULL OR
        general_checkout_id IS NOT NULL OR
        reason              IS NOT NULL
    )
);
CREATE INDEX ON stock_adjustments (inventory_entry_id);
CREATE INDEX ON stock_adjustments (order_line_item_id)  WHERE order_line_item_id IS NOT NULL;
CREATE INDEX ON stock_adjustments (build_id)            WHERE build_id IS NOT NULL;
CREATE INDEX ON stock_adjustments (general_checkout_id) WHERE general_checkout_id IS NOT NULL;


-- =============================================================
-- PROJECTS & BOMs
-- =============================================================

CREATE TABLE projects (
    id                     UUID           PRIMARY KEY DEFAULT uuidv7(),
    team_id                UUID           NOT NULL REFERENCES teams(id),
    name                   TEXT           NOT NULL,
    description            TEXT,
    status                 project_status NOT NULL DEFAULT 'active',
    active_bom_revision_id UUID,          -- nullable; FK added below; set after first BOM import
    bom_import_config      JSONB,         -- last-used CSV column-mapping config; NULL until first import
    created_by             UUID           NOT NULL REFERENCES users(id),
    created_at             TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);
CREATE INDEX ON projects (team_id);

CREATE TABLE bom_revisions (
    id          UUID        PRIMARY KEY DEFAULT uuidv7(),
    project_id  UUID        NOT NULL REFERENCES projects(id),
    label       TEXT        NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes       TEXT,
    imported_by UUID        NOT NULL REFERENCES users(id)
);
CREATE INDEX ON bom_revisions (project_id);

-- Resolve the forward reference from projects to bom_revisions
ALTER TABLE projects
    ADD CONSTRAINT projects_active_bom_revision_fk
    FOREIGN KEY (active_bom_revision_id) REFERENCES bom_revisions(id);

CREATE TABLE bom_line_items (
    id                    UUID PRIMARY KEY DEFAULT uuidv7(),
    bom_revision_id       UUID NOT NULL REFERENCES bom_revisions(id),
    reference_designators TEXT NOT NULL,  -- e.g. "R1, R3, R7"
    quantity              INT  NOT NULL CHECK (quantity > 0),
    raw_description       TEXT,
    resolved_part_id      UUID REFERENCES parts(id)  -- NULL until resolved
);
CREATE INDEX ON bom_line_items (bom_revision_id);
CREATE INDEX ON bom_line_items (resolved_part_id) WHERE resolved_part_id IS NOT NULL;

-- Many-to-many: approved substitute parts per BOM line item
CREATE TABLE bom_line_item_substitutes (
    bom_line_item_id UUID NOT NULL REFERENCES bom_line_items(id),
    part_id          UUID NOT NULL REFERENCES parts(id),
    PRIMARY KEY (bom_line_item_id, part_id)
);


-- =============================================================
-- SHOPPING CART
-- =============================================================

CREATE TABLE shopping_carts (
    id      UUID PRIMARY KEY DEFAULT uuidv7(),
    team_id UUID NOT NULL UNIQUE REFERENCES teams(id)
);

CREATE TABLE cart_line_items (
    id                      UUID             PRIMARY KEY DEFAULT uuidv7(),
    shopping_cart_id        UUID             NOT NULL REFERENCES shopping_carts(id),
    part_id                 UUID             NOT NULL REFERENCES parts(id),
    quantity                INT              NOT NULL CHECK (quantity > 0),
    listing_id              UUID             REFERENCES distributor_listings(id),
    source                  cart_item_source NOT NULL,
    source_bom_line_item_id UUID             REFERENCES bom_line_items(id),
    -- bom_shortfall items must carry a reference to the originating line item
    CONSTRAINT bom_shortfall_requires_line_item CHECK (
        source != 'bom_shortfall' OR source_bom_line_item_id IS NOT NULL
    ),
    UNIQUE (shopping_cart_id, part_id)
);
CREATE INDEX ON cart_line_items (shopping_cart_id);


-- =============================================================
-- BUILDS
-- =============================================================

CREATE TABLE builds (
    id              UUID        PRIMARY KEY DEFAULT uuidv7(),
    team_id         UUID        NOT NULL REFERENCES teams(id),
    project_id      UUID        NOT NULL REFERENCES projects(id),
    bom_revision_id UUID        NOT NULL REFERENCES bom_revisions(id),
    boards_built    INT         NOT NULL CHECK (boards_built > 0),
    built_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes           TEXT,
    recorded_by     UUID        NOT NULL REFERENCES users(id)
);
CREATE INDEX ON builds (team_id);


-- =============================================================
-- GENERAL CHECKOUTS
-- =============================================================

CREATE TABLE general_checkouts (
    id           UUID        PRIMARY KEY DEFAULT uuidv7(),
    team_id      UUID        NOT NULL REFERENCES teams(id),
    performed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason       TEXT,
    performed_by UUID        NOT NULL REFERENCES users(id)
);
CREATE INDEX ON general_checkouts (team_id);

CREATE TABLE general_checkout_line_items (
    id                  UUID PRIMARY KEY DEFAULT uuidv7(),
    general_checkout_id UUID NOT NULL REFERENCES general_checkouts(id),
    part_id             UUID NOT NULL REFERENCES parts(id),
    quantity            INT  NOT NULL CHECK (quantity > 0)
);
CREATE INDEX ON general_checkout_line_items (general_checkout_id);


-- =============================================================
-- ORDERS
-- =============================================================

CREATE TABLE orders (
    id                     UUID                    PRIMARY KEY DEFAULT uuidv7(),
    team_id                UUID                    NOT NULL REFERENCES teams(id),
    distributor_id         UUID                    NOT NULL REFERENCES distributors(id),
    status                 order_status            NOT NULL DEFAULT 'draft',
    submission_method      order_submission_method,  -- NULL until exported or submitted
    external_reference     TEXT,
    external_ref_linked_at TIMESTAMPTZ,
    received_at            TIMESTAMPTZ,
    notes                  TEXT,
    created_by             UUID                    NOT NULL REFERENCES users(id),
    created_at             TIMESTAMPTZ             NOT NULL DEFAULT NOW()
);
CREATE INDEX ON orders (team_id);
CREATE INDEX ON orders (status);

CREATE TABLE order_line_items (
    id                        UUID           PRIMARY KEY DEFAULT uuidv7(),
    order_id                  UUID           NOT NULL REFERENCES orders(id),
    listing_id                UUID           NOT NULL REFERENCES distributor_listings(id),
    quantity_ordered          INT            NOT NULL CHECK (quantity_ordered > 0),
    quantity_received         INT            NOT NULL DEFAULT 0 CHECK (quantity_received >= 0),
    unit_price_snapshot       NUMERIC(14, 4) NOT NULL,
    fixed_order_cost_snapshot NUMERIC(14, 4) NOT NULL DEFAULT 0,
    CONSTRAINT received_not_exceeds_ordered CHECK (
        quantity_received <= quantity_ordered
    )
);
CREATE INDEX ON order_line_items (order_id);


-- =============================================================
-- BACK-FILL FKs ON stock_adjustments
-- =============================================================

ALTER TABLE stock_adjustments
    ADD CONSTRAINT sa_order_line_item_fk
        FOREIGN KEY (order_line_item_id) REFERENCES order_line_items(id),
    ADD CONSTRAINT sa_build_fk
        FOREIGN KEY (build_id) REFERENCES builds(id),
    ADD CONSTRAINT sa_general_checkout_fk
        FOREIGN KEY (general_checkout_id) REFERENCES general_checkouts(id);


-- =============================================================
-- ADMIN FEATURES
-- =============================================================

-- Instance-level admin flag.
-- The first admin must be bootstrapped directly:
--   UPDATE users SET is_admin = TRUE WHERE email = 'your@email.com';
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- One-time password reset tokens generated by admins and shared out-of-band.
CREATE TABLE password_reset_tokens (
    id         UUID        PRIMARY KEY DEFAULT uuidv7(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

CREATE INDEX idx_reset_tokens_user ON password_reset_tokens(user_id);


-- =============================================================
-- SEED DATA
-- =============================================================

INSERT INTO distributors (name, website, api_type) VALUES
    ('DigiKey',        'https://www.digikey.com', 'digikey'),
    ('TestDistributor', NULL,                     'test');

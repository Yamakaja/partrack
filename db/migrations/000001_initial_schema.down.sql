-- Drop in reverse dependency order

DELETE FROM distributors WHERE api_type IN ('digikey', 'test');

DROP TABLE IF EXISTS password_reset_tokens;

ALTER TABLE users DROP COLUMN IF EXISTS is_admin;

ALTER TABLE stock_adjustments
    DROP CONSTRAINT IF EXISTS sa_order_line_item_fk,
    DROP CONSTRAINT IF EXISTS sa_build_fk,
    DROP CONSTRAINT IF EXISTS sa_general_checkout_fk;

ALTER TABLE projects
    DROP CONSTRAINT IF EXISTS projects_active_bom_revision_fk;

DROP TABLE IF EXISTS
    order_line_items,
    orders,
    general_checkout_line_items,
    general_checkouts,
    builds,
    cart_line_items,
    shopping_carts,
    bom_line_item_substitutes,
    bom_line_items,
    bom_revisions,
    projects,
    stock_adjustments,
    inventory_entries,
    storage_locations,
    distributor_listings,
    part_parameters,
    parts,
    distributor_configs,
    distributors,
    api_tokens,
    sessions,
    user_preferences,
    user_credentials,
    oidc_identities,
    team_memberships,
    teams,
    users
CASCADE;

DROP TYPE IF EXISTS
    cart_item_source,
    order_submission_method,
    order_status,
    project_status,
    parameter_source_type,
    part_lifecycle_status,
    part_scope,
    team_role;

DROP EXTENSION IF EXISTS pg_uuidv7;

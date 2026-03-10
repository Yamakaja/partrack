-- Enable trigram extension for ILIKE text search support
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Text search: parts (mpn, manufacturer, description)
-- These columns are used with ILIKE '%...%' in SearchParts, ListParts, ListPartsWithInventory, ListInventory
CREATE INDEX IF NOT EXISTS idx_parts_mpn_trgm ON parts USING GIN (mpn gin_trgm_ops) WHERE mpn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_parts_manufacturer_trgm ON parts USING GIN (manufacturer gin_trgm_ops) WHERE manufacturer IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_parts_description_trgm ON parts USING GIN (description gin_trgm_ops) WHERE description IS NOT NULL;

-- Text search: part_aliases (alias)
-- Used in SearchParts via EXISTS subquery with ILIKE
CREATE INDEX IF NOT EXISTS idx_part_aliases_alias_trgm ON part_aliases USING GIN (alias gin_trgm_ops);

-- Text search: projects (name)
-- Used in SearchProjects and SearchBuilds
CREATE INDEX IF NOT EXISTS idx_projects_name_trgm ON projects USING GIN (name gin_trgm_ops);

-- Text search: storage_locations (label)
-- Used in SearchStorageLocations
CREATE INDEX IF NOT EXISTS idx_storage_locations_label_trgm ON storage_locations USING GIN (label gin_trgm_ops);

-- Text search: orders (purchase_order_number, external_reference)
-- Used in SearchOrders
CREATE INDEX IF NOT EXISTS idx_orders_po_number_trgm ON orders USING GIN (purchase_order_number gin_trgm_ops) WHERE purchase_order_number IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orders_ext_ref_trgm ON orders USING GIN (external_reference gin_trgm_ops) WHERE external_reference IS NOT NULL;

-- inventory_entries: storage_location_id
-- Used in CountInventoryEntriesForLocation and ListInventory storage filter
CREATE INDEX IF NOT EXISTS idx_inventory_entries_storage_location_id ON inventory_entries (storage_location_id) WHERE storage_location_id IS NOT NULL;

-- order_line_items: listing_id
-- Used in GetOnOrderQtysForParts, GetUnallocatedOnOrderForParts, GetOrdersWithQtyForPart,
-- GetOutstandingOrderedQtyForBuildPart — all join listing_id → distributor_listings to filter by part_id
CREATE INDEX IF NOT EXISTS idx_order_line_items_listing_id ON order_line_items (listing_id);

-- builds: project_id
-- Used in ListBuilds optional project_id filter
CREATE INDEX IF NOT EXISTS idx_builds_project_id ON builds (project_id);

-- bom_line_item_substitutes: part_id
-- PK is (bom_line_item_id, part_id); part_id-only lookups needed by GetInventoryQuantitiesForRevision
CREATE INDEX IF NOT EXISTS idx_bom_line_item_substitutes_part_id ON bom_line_item_substitutes (part_id);

-- orders: distributor_id
-- Used as optional filter in ListOrders / CountOrders
CREATE INDEX IF NOT EXISTS idx_orders_distributor_id ON orders (distributor_id) WHERE distributor_id IS NOT NULL;

-- part_parameters: source_distributor_id
-- Used in DeleteDistributorParameters
CREATE INDEX IF NOT EXISTS idx_part_parameters_source_distributor_id ON part_parameters (source_distributor_id) WHERE source_distributor_id IS NOT NULL;

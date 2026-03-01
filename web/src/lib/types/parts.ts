export type PartScope = 'global' | 'team';
export type LifecycleStatus = 'active' | 'nrnd' | 'obsolete' | 'deprecated' | 'no_longer_available';

export interface Part {
  id: string;
  scope: PartScope;
  team_id: string | null;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  category: string | null;
  package: string | null;
  datasheet_url: string | null;
  lifecycle_status: LifecycleStatus | null;
  enrichment_source_id: string | null;
  enriched_at: string | null;
  preferred_parameter_source_id: string | null;
  created_at: string;
  // Inventory fields — present when team_id is passed to GET /parts/:id
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  minimum_stock_quantity: number | null;
  reorder_quantity: number | null;
  preferred_listing_id: string | null;
  storage_location_id: string | null;
  storage_location_label: string | null;
  // Embedded from GET /parts/:id
  listings: Listing[];
  parameters: {
    user_specified: Parameter[];
    distributor_sourced: Parameter[];
  };
}

export interface PartSummary {
  id: string;
  scope: PartScope;
  team_id: string | null;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  category: string | null;
  package: string | null;
  lifecycle_status: LifecycleStatus | null;
  created_at: string;
  // Inventory fields — present when team_id is passed to GET /parts
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  minimum_stock_quantity: number | null;
  reorder_quantity: number | null;
  storage_location_label: string | null;
}

export interface Parameter {
  id: string;
  part_id: string;
  name: string;
  value: string;
  unit: string | null;
  source_type: 'user' | 'distributor';
  source_distributor_id: string | null;
  source_distributor_name: string | null;
}

export interface Listing {
  id: string;
  part_id?: string;
  distributor_id: string;
  distributor_name: string;
  sku: string;
  packaging_type: string | null;
  unit_price: string | null;
  stock_available: number | null;
  moq: number;
  fixed_order_cost: string;
  last_refreshed_at: string | null;
}

export interface CredentialField {
  key: string;
  label: string;
  description?: string;
  placeholder?: string;
  secret: boolean;
  required: boolean;
  type?: 'string' | 'bool';
}

export interface Distributor {
  id: string;
  name: string;
  website: string | null;
  api_type: string | null;
}

export interface DistributorConfig {
  id: string;
  distributor_id: string;
  distributor_name: string;
  enabled: boolean;
  preferred: boolean;
  cache_duration_seconds: number;
  has_credentials: boolean;
}

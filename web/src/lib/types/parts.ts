export type PartScope = 'global' | 'team';
export type LifecycleStatus = 'active' | 'nrnd' | 'obsolete' | 'deprecated' | 'no_longer_available';

export interface Part {
  id: string;
  scope: PartScope;
  team_id: string | null;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  short_description: string | null;
  /** @deprecated use category_id + category_name instead */
  category: string | null;
  category_id: number | null;
  category_name: string | null;
  category_source: string;
  /** parent category id, present on GET /parts/:id when team_id is provided */
  category_parent_id: number | null;
  package: string | null;
  datasheet_url: string | null;
  product_url: string | null;
  image_url: string | null;
  lifecycle_status: LifecycleStatus | null;
  enrichment_source_id: string | null;
  enriched_at: string | null;
  preferred_parameter_source_id: string | null;
  created_at: string;
  aliases: string[];
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
  short_description: string | null;
  /** @deprecated use category_id + category_name instead */
  category: string | null;
  category_id: number | null;
  category_name: string | null;
  category_source: string;
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
  part_id?: string;
  name: string;
  value: string;
  unit: string | null;
  source_type: 'user' | 'distributor';
  source_distributor_id?: string | null;
  source_distributor_name?: string | null;
  // Populated when a partrack parameter mapping exists
  parameter_id: number | null;
  parameter_slug: string | null;
  parameter_display_name: string | null;
  parameter_unit_symbol: string | null;
  numeric_value: number | null;
  numeric_value_max: number | null;
}

export interface ParameterDefinition {
  id: number;
  slug: string;
  display_name: string;
  unit: string | null;
  unit_symbol: string | null;
  value_type: 'numeric' | 'range' | 'text' | 'enum';
}

export interface DistributorParamMapping {
  id: string;
  distributor_id: string;
  distributor_name: string;
  distributor_param_id: string;
  distributor_param_name: string;
  parameter_id: number | null;
  ignored: boolean;
  part_count: number;
  sample_values: string[];
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
  price_breaks?: { break_qty: number; unit_price: string }[];
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

export interface DistributorRateLimit {
  limit: number;
  remaining: number;
  updated_at: string;
}

export interface DistributorConfig {
  id: string;
  distributor_id: string;
  distributor_name: string;
  enabled: boolean;
  preferred: boolean;
  cache_duration_seconds: number;
  has_credentials: boolean;
  api_credentials?: Record<string, unknown> | null;
  rate_limit?: DistributorRateLimit | null;
}

export interface CategoryParameter {
  id: number;
  slug: string;
  display_name: string;
  unit: string | null;
  unit_symbol: string | null;
  value_type: 'numeric' | 'text' | 'enum';
  min_numeric: number | null;
  max_numeric: number | null;
  enum_values: string[];
}

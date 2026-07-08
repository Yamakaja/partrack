export type OrderStatus =
  | 'draft'
  | 'exported'
  | 'submitted'
  | 'placed'
  | 'partially_received'
  | 'received'
  | 'cancelled';

export type CartItemSource = 'bom_shortfall' | 'inventory_maintenance' | 'manual';

export type BuildStatus = 'draft' | 'pending' | 'ordered' | 'ready' | 'assembled' | 'cancelled';

export interface PriceBreak {
  break_qty: number;
  unit_price: string;
}

export interface CartListing {
  listing_id: string;
  distributor_id: string;
  distributor_name: string;
  sku: string;
  unit_price: string | null;
  fixed_order_cost: string | null;
  moq: number;
  price_breaks: PriceBreak[];
  packaging_type: string | null;
  stock_available: number | null;
  last_refreshed_at: string | null;
  optimized_qty: number;
  chosen: boolean;
  /** Why this listing was auto-selected under the sourcing policy (absent when user-chosen). */
  reason?: string;
  /** Preference honoured on missing/stale stock data — surface a warning. */
  preferred_stale?: boolean;
}

export interface BuildDemand {
  build_id: string;
  project_name: string;
  bom_revision_label: string;
  quantity_needed: number;
}

export interface CartItem {
  id: string;
  part_id: string;
  part_mpn: string | null;
  part_description: string | null;
  part_short_description: string | null;
  part_manufacturer: string | null;
  part_category: string | null;
  part_package: string | null;
  part_product_url: string | null;
  part_datasheet_url: string | null;
  part_image_url: string | null;
  quantity: number;
  listing: CartListing | null;
  source: CartItemSource;
  build_demands: BuildDemand[];
}

export interface Cart {
  items: CartItem[];
}

export interface QuoteItem {
  sku: string;
  customer_reference: string;
  unit_price: string;
  quantity_available: number;
  is_obsolete: boolean;
  is_discontinued: boolean;
  expiration_date: string | null;
}

export interface DigiKeyStatusShipment {
  quantity_shipped: number;
  invoice_id: number;
  shipped_date?: string;
  tracking_number: string;
  expected_delivery_date?: string;
}

export interface DigiKeyStatusSchedule {
  scheduled_date?: string;
  quantity_scheduled: number;
  release_date?: string;
}

export interface DigiKeyStatusLineItem {
  sku: string;
  mpn: string;
  pack_type?: string;
  unit_price: number;
  quantity_ordered: number;
  quantity_shipped: number;
  quantity_back_order: number;
  quantity_reserved: number;
  shipments: DigiKeyStatusShipment[];
  schedules: DigiKeyStatusSchedule[];
}

export interface DigiKeyAddress {
  line1?: string;
  line2?: string;
  city?: string;
  state?: string;
  zip_code?: string;
  company_name?: string;
  phone?: string;
  iso_code?: string;
}

export interface DigiKeyContact {
  first_name?: string;
  last_name?: string;
  email?: string;
}

export interface UnresolvedImportItem {
  sku: string;
  mpn?: string;
  unit_price?: number;
  quantity_ordered: number;
  quantity_shipped: number;
  quantity_backordered: number;
  failure_reason?: 'enrichment_failed' | 'enrichment_nil' | 'listing_not_found';
}

export interface DigiKeyIntegration {
  list_id?: string;
  list_url?: string;
  quote_id?: number;
  quote_name?: string;
  quote_items?: QuoteItem[];
  last_status?: string;
  last_status_detail?: string;
  last_status_checked_at?: string;
  last_status_order_number?: number;
  last_status_currency?: string;
  last_status_total_price?: number;
  last_status_ship_method?: string;
  last_status_shipping_address?: DigiKeyAddress;
  last_status_contact?: DigiKeyContact;
  last_status_line_items?: DigiKeyStatusLineItem[];
  unresolved_items?: UnresolvedImportItem[];
}

export interface OrderIntegrations {
  digikey?: DigiKeyIntegration;
}

export interface Order {
  id: string;
  team_id: string;
  distributor_id: string;
  distributor_name: string;
  distributor_api_type: string | null;
  status: OrderStatus;
  external_reference: string | null;
  purchase_order_number: string | null;
  notes: string | null;
  submission_method: 'api' | 'manual' | null;
  external_ref_linked_at: string | null;
  received_at: string | null;
  created_at: string;
  total_price: string | null;
  line_item_count: number;
  created_by_display_name: string;
  integrations: OrderIntegrations;
}

export interface OrderLineItem {
  id: string;
  order_id: string;
  listing_id: string;
  part_id: string;
  part_mpn: string | null;
  part_manufacturer: string | null;
  part_description: string | null;
  distributor_name: string;
  sku: string;
  packaging_type: string | null;
  quantity_ordered: number;
  quantity_received: number;
  unit_price_snapshot: string;
  fixed_order_cost_snapshot: string;
}

export interface LinkedBuild {
  id: string;
  project_id: string;
  project_name: string;
  bom_revision_id: string;
  bom_revision_label: string;
  quantity: number;
  status: BuildStatus;
  created_at: string;
  created_by_display_name: string;
  total_line_items: number;
  fulfilled_line_items: number;
}

export interface Build {
  id: string;
  team_id: string;
  project_id: string;
  project_name: string;
  bom_revision_id: string;
  bom_revision_label: string;
  quantity: number;
  assembled_quantity?: number;
  status: BuildStatus;
  created_at: string;
  created_by: string;
  created_by_display_name?: string;
  assembled_at?: string;
  assembled_by_display_name?: string;
  notes?: string;
  excluded_from_cart?: boolean;
  selections_checked_at?: string | null;
  selections_stale?: boolean;
  substitute_selections?: import('./projects.js').SubstituteSelection[];
  excluded_designators?: string[];
  line_item_overrides?: { bom_line_item_id: string; override_type: 'use_available' | 'skip' }[];
  // Most recent non-abandoned kitting session for this build, if any (for the desktop
  // kitting panel + checkout hand-off). Null when the build has never been kitted.
  kitting_session?: { id: string; status: 'in_progress' | 'completed' } | null;
}

export interface BuildPartAllocation {
  part_id: string;
  part_mpn?: string;
  quantity_needed: number;
  quantity_reserved: number;
  quantity_received: number;
}

export interface BuildConsumedItem {
  inventory_entry_id: string;
  part_id: string;
  part_mpn: string | null;
  quantity_deducted: number;
}

export interface GeneralCheckout {
  id: string;
  team_id: string;
  checked_out_at: string;
  notes: string | null;
  created_by_display_name: string;
  items: CheckoutItem[];
}

export interface CheckoutItem {
  id: string;
  part_id: string;
  part_mpn: string | null;
  quantity: number;
  inventory_entry_id: string;
}

export interface PartShortfall {
  part_id: string;
  mpn: string;
  description: string;
  quantity_from_order_before: number;
  actual_qty: number;
}

export interface AffectedBuild {
  build_id: string;
  project_name: string;
  build_quantity: number;
  status: string;
  suggested_keep: boolean;
  parts_short: PartShortfall[];
}

export interface AllocationConflicts {
  affected_builds: AffectedBuild[];
}

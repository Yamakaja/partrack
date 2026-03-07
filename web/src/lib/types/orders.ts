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
  quantity: number;
  listing: CartListing | null;
  source: CartItemSource;
  build_demands: BuildDemand[];
}

export interface Cart {
  items: CartItem[];
}

export interface Order {
  id: string;
  team_id: string;
  distributor_id: string;
  distributor_name: string;
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
}

export interface OrderLineItem {
  id: string;
  order_id: string;
  listing_id: string;
  part_id: string;
  part_mpn: string | null;
  distributor_name: string;
  sku: string;
  packaging_type: string | null;
  quantity_ordered: number;
  quantity_received: number;
  unit_price_snapshot: string;
  fixed_order_cost_snapshot: string;
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
  created_by?: string;
  assembled_at?: string;
  assembled_by_display_name?: string;
  notes?: string;
  excluded_from_cart?: boolean;
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

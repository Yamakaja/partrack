export type OrderStatus =
  | 'draft'
  | 'placed'
  | 'exported'
  | 'partially_received'
  | 'received';

export type CartItemSource = 'bom_shortfall' | 'inventory_maintenance' | 'manual';

export interface PriceBreak {
  break_qty: number;
  unit_price: string;
}

export interface ResolvedListing {
  listing_id: string;
  distributor_id: string;
  distributor_name: string;
  sku: string;
  unit_price: string | null;
  fixed_order_cost: string | null;
  moq: number;
}

export interface PendingBuild {
  bom_revision_id: string;
  revision_label: string;
  project_name: string;
  build_quantity: number;
}

export interface CartItem {
  id: string;
  part_id: string;
  part_mpn: string | null;
  part_description: string | null;
  quantity: number;
  listing_id: string | null;
  listing_sku: string | null;
  listing_distributor_name: string | null;
  unit_price: string | null;
  price_breaks: PriceBreak[];
  fixed_order_cost: string | null;
  source: CartItemSource;
  resolved_listing: ResolvedListing | null;
}

export interface Cart {
  items: CartItem[];
  pending_builds: PendingBuild[];
}

export interface Order {
  id: string;
  team_id: string;
  distributor_id: string;
  distributor_name: string;
  status: OrderStatus;
  distributor_order_ref: string | null;
  created_at: string;
  updated_at: string;
  total_price: string | null;
  line_item_count: number;
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
  quantity_built: number;
  built_at: string;
  notes: string | null;
  built_by_display_name: string;
}

export interface BuildConsumedItem {
  bom_line_item_id: string;
  reference_designators: string;
  part_id: string;
  part_mpn: string | null;
  quantity_deducted: number;
  inventory_entry_id: string;
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

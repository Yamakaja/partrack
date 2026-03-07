export interface StorageLocation {
  id: string;
  team_id: string;
  label: string;
  description: string | null;
}

export interface InventoryEntry {
  id: string;
  team_id: string;
  part_id: string;
  part_mpn: string | null;
  part_description: string | null;
  quantity_on_hand: number;
  storage_location_id: string | null;
  storage_location_label: string | null;
  preferred_listing_id: string | null;
  minimum_stock_quantity: number | null;
  reorder_quantity: number | null;
}

export interface StockAdjustment {
  id: string;
  delta: number;
  adjusted_at: string;
  reason: string | null;
  created_by: string;
  created_by_display_name: string;
  source_type: 'order_line_item' | 'build' | 'general_checkout' | null;
  source_id: string | null;
  source_order_id: string | null;
}

export interface ReplenishPreviewItem {
  inventory_entry_id: string;
  part_id: string;
  part_mpn: string | null;
  quantity_on_hand: number;
  minimum_stock_quantity: number;
  shortfall: number;
  reorder_quantity: number | null;
  quantity_to_add_to_cart: number;
  preferred_listing_id: string | null;
}

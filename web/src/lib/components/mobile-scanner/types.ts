export interface ScanPart {
  id: string;
  mpn: string | null;
  description: string | null;
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  storage_location: string | null;
  // Optional fields: populated from bag scans only
  manufacturer?: string | null;
  sku?: string | null;
  distributor_name?: string | null;
  distributor?: string | null;
  barcode_qty?: number;
}

export interface ScanOrder {
  id: string;
  title: string;
  title_is_fallback: boolean;
  external_ref: string | null;
  notes: string | null;
  status: string;
  distributor_name: string;
  item_count: number;
  items_fully_received: number;
  total_qty_ordered: number;
  total_qty_received: number;
  total_value: string | null;
  created_by_name: string;
  created_by_me: boolean;
  created_at: string;
}

export type NavEntry =
  | { screen: 'menu' }
  | { screen: 'receive_order' }
  | { screen: 'bag_scan' }
  | { screen: 'find_part'; initialQuery: string }
  | { screen: 'part_detail'; part: ScanPart; initialAdjust?: { value: number; mode: 'set' | 'adjust'; reason?: string } }
  | { screen: 'distributor_search'; distributorId: string; distributorName: string; initialQuery: string; parsedQty: number | null }
  | { screen: 'stock_adjustment'; part: ScanPart; mode: 'set' | 'adjust'; initialValue?: number; initialReason?: string };

export interface BagLookupResult {
  id: string;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  sku: string | null;
  distributor_name: string | null;
  distributor: string | null;
  barcode_qty: number;
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  storage_location: string | null;
}

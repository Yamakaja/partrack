export interface ScanPart {
  id: string;
  mpn: string | null;
  description: string | null;
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  storage_location: string | null;
}

export interface ScanOrder {
  id: string;
  ref: string;
  status: string;
  distributor_name: string;
  item_count: number;
}

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

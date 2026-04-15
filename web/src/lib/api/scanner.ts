import type { ScanPart, ScanOrder, PartLocation } from '$lib/components/mobile-scanner/types.js';
import type { StorageLocation } from '$lib/types/inventory.js';

// ── Error ─────────────────────────────────────────────────────────────────────

export class ScanExpiredError extends Error {
  constructor() {
    super('Scan session expired');
    this.name = 'ScanExpiredError';
  }
}

// ── Base request ──────────────────────────────────────────────────────────────

async function scanRequest<T>(
  token: string,
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401 || res.status === 410) throw new ScanExpiredError();
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  if (res.status === 204) return undefined as T;
  return res.json();
}

// ── Response types ────────────────────────────────────────────────────────────

export interface PartDetailListing {
  id: string;
  distributor_name: string;
  sku: string;
  packaging_type: string;
  unit_price?: string | null;
  stock_available?: number | null;
  moq: number;
}

export interface PartDetailParam {
  name: string;
  value: string;
  unit?: string | null;
}

export interface PartDetailResponse {
  id: string;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  package: string | null;
  lifecycle_status: string | null;
  datasheet_url: string | null;
  product_url: string | null;
  image_url: string | null;
  category_name: string | null;
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  quantity_reserved: number;
  quantity_available: number;
  storage_location: PartLocation | null;
  minimum_stock_quantity: number | null;
  reorder_quantity: number | null;
  listings: PartDetailListing[];
  parameters: PartDetailParam[];
}

// Bag lookup — discriminated union on status

export interface BagLookupFound {
  status: 'found';
  id: string;
  mpn: string | null;
  description: string | null;
  inventory_entry_id: string | null;
  quantity_on_hand: number | null;
  storage_location: PartLocation | null;
  manufacturer: string | null;
  sku: string | null;
  distributor_name: string | null;
  distributor: string | null;
  barcode_qty: number;
  bag_qty: number;
  inventory_entry_newly_created: boolean;
}

export interface BagLookupNotFoundInDb {
  status: 'not_found_in_db';
  parsed_mpn: string;
  parsed_manufacturer: string;
  parsed_sku: string;
  parsed_distributor: string;
  parsed_distributor_id: string;
  preferred_distributor_id: string;
  preferred_distributor_name: string;
  parsed_qty: number | null;
  parsed_lot: string;
  can_auto_import: boolean;
  distributor_configs: Array<{ id: string; name: string }>;
}

export interface BagLookupNoFields {
  status: 'no_fields';
  parsed_mpn: string;
  parsed_sku: string;
  parsed_distributor: string;
  parsed_qty: number | null;
  parsed_lot: string;
}

export interface BagLookupParseError {
  status: 'parse_error';
  raw: string;
}

export type BagLookupResponse =
  | BagLookupFound
  | BagLookupNotFoundInDb
  | BagLookupNoFields
  | BagLookupParseError;

// Order scan — discriminated union on status

export interface ScanMatchedResponse {
  status: 'matched';
  line_item_id: string;
  part_name: string;
  mpn: string;
  sku: string;
  barcode_qty: number;
  quantity_ordered: number;
  already_received: number;
  over_receive: boolean;
}

export interface ScanNoMatchResponse {
  status: 'no_match';
  parsed_mpn: string;
  parsed_sku: string;
}

export interface ScanParseErrorResponse {
  status: 'parse_error';
}

export type ScanResponse = ScanMatchedResponse | ScanNoMatchResponse | ScanParseErrorResponse;

export interface DistributorSearchItem {
  mpn: string;
  manufacturer: string;
  description: string;
  category: string;
  primary_sku: string;
  package: string;
  unit_price: string;
  stock_available: number | null;
}

export interface StockAdjustmentResponse {
  ok: boolean;
  new_quantity_on_hand: number;
}

export interface ConfirmReceiptResponse {
  order_status: 'received' | 'pending';
  inventory_entry_id: string;
  storage_location: PartLocation | null;
}

// ── API functions ─────────────────────────────────────────────────────────────

export function checkSession(token: string): Promise<unknown> {
  return scanRequest<unknown>(token, 'GET', '/api/v1/scan/session');
}

export function searchParts(token: string, q: string): Promise<{ parts: ScanPart[] }> {
  return scanRequest(token, 'GET', `/api/v1/scan/parts?q=${encodeURIComponent(q)}`);
}

export function getPartDetail(token: string, partId: string): Promise<PartDetailResponse> {
  return scanRequest(token, 'GET', `/api/v1/scan/parts/${partId}`);
}

export function lookupBag(token: string, barcode: string): Promise<BagLookupResponse> {
  return scanRequest(token, 'POST', '/api/v1/scan/lookup', { barcode });
}

export function importScanPart(
  token: string,
  body: { distributor_id: string; sku: string },
): Promise<{ part_id: string }> {
  return scanRequest(token, 'POST', '/api/v1/scan/parts/import', body);
}

export interface ImportMultiResult {
  part_id: string;
  failed: Array<{ distributor_id: string; sku: string; reason: string }>;
}

export function importScanPartMulti(
  token: string,
  body: { listings: Array<{ distributor_id: string; sku: string }> },
): Promise<ImportMultiResult> {
  return scanRequest(token, 'POST', '/api/v1/scan/parts/import-multi', body);
}

export function stockAdjustment(
  token: string,
  body: { inventory_entry_id: string; delta: number; reason?: string },
): Promise<StockAdjustmentResponse> {
  return scanRequest(token, 'POST', '/api/v1/scan/stock-adjustment', body);
}

export function listOrders(token: string): Promise<{ orders: ScanOrder[] }> {
  return scanRequest(token, 'GET', '/api/v1/scan/orders');
}

export function scanBarcode(
  token: string,
  body: { order_id: string; barcode: string },
): Promise<ScanResponse> {
  return scanRequest(token, 'POST', '/api/v1/scan/scans', body);
}

export function confirmReceipt(token: string, qty: number): Promise<ConfirmReceiptResponse> {
  return scanRequest(token, 'POST', '/api/v1/scan/confirm', { qty });
}

export function skipItem(token: string): Promise<void> {
  return scanRequest<void>(token, 'POST', '/api/v1/scan/skip');
}

export function searchDistributor(
  token: string,
  distributorId: string,
  q: string,
): Promise<{ items: DistributorSearchItem[] }> {
  return scanRequest(
    token,
    'GET',
    `/api/v1/scan/distributor-search?distributor_id=${encodeURIComponent(distributorId)}&q=${encodeURIComponent(q)}`,
  );
}

export function listScanStorageLocations(token: string): Promise<{ locations: StorageLocation[] }> {
  return scanRequest(token, 'GET', '/api/v1/scan/storage-locations');
}

export interface StorageInventoryEntry {
  inventory_entry_id: string;
  part_id: string;
  mpn: string | null;
  description: string | null;
  quantity_on_hand: number;
  storage_location: PartLocation | null;
}

export function listScanStorageInventory(
  token: string,
  locationId: string,
): Promise<StorageInventoryEntry[]> {
  return scanRequest(token, 'GET', `/api/v1/scan/storage/${locationId}/inventory`);
}

export function patchInventoryEntry(
  token: string,
  inventoryEntryId: string,
  storageLocation: PartLocation | null,
): Promise<{ id: string; storage_location: PartLocation | null }> {
  return scanRequest(token, 'PATCH', `/api/v1/scan/inventory-entries/${inventoryEntryId}`, {
    storage_location: storageLocation,
  });
}

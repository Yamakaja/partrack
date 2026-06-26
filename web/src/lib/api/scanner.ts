import type { ScanPart, ScanOrder, PartLocation } from '$lib/components/mobile-scanner/types.js';
import type { StorageLocation } from '$lib/types/inventory.js';

// ── Error ─────────────────────────────────────────────────────────────────────

export class ScanExpiredError extends Error {
  constructor() {
    super('Scan session expired');
    this.name = 'ScanExpiredError';
  }
}

// Thrown for non-OK responses (other than expiry). Carries the parsed error body so
// callers can react to a specific code (e.g. STALE_STOCK with current_quantity).
export class ScanRequestError extends Error {
  status: number;
  code?: string;
  body?: Record<string, unknown>;
  constructor(status: number, code?: string, body?: Record<string, unknown>) {
    super(code ? `${code} (HTTP ${status})` : `HTTP ${status}`);
    this.name = 'ScanRequestError';
    this.status = status;
    this.code = code;
    this.body = body;
  }
}

// ── Base request ──────────────────────────────────────────────────────────────

// The phone authenticates with the HttpOnly `partrack_scan` cookie set at pairing —
// the durable token is never held in JS (so it is not XSS-exfiltratable). `credentials:
// 'same-origin'` sends that cookie.
async function scanRequest<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: {
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401 || res.status === 410) throw new ScanExpiredError();
  if (!res.ok) {
    let errBody: Record<string, unknown> | undefined;
    try {
      errBody = await res.json();
    } catch {
      // non-JSON error body
    }
    const code = typeof errBody?.code === 'string' ? errBody.code : undefined;
    throw new ScanRequestError(res.status, code, errBody);
  }
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
  audit_log_id: string | null;
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

export interface BulkSessionInfo {
  id: string;
  item_count: number;
  state: 'active' | 'timed_out';
}

export interface ScanSessionState {
  id: string;
  team_id: string;
  phone_connected: boolean;
  active_kitting_session_id: string | null;
  active_kitting_build_ref: string | null;
  bulk_session: BulkSessionInfo | null;
}

// pairScanSession exchanges a single-use pairing ticket (from the QR code) for
// the durable scan token. Unauthenticated — the ticket is the credential.
export async function pairScanSession(
  ticket: string,
): Promise<{ token: string; session_id: string; team_id: string; expires_at: string }> {
  const res = await fetch('/api/v1/scan/pair', {
    method: 'POST',
    credentials: 'same-origin', // store the HttpOnly scan cookie the response sets
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ticket }),
  });
  if (!res.ok) throw new ScanExpiredError(); // invalid/expired/spent pairing ticket
  return res.json();
}

// createStreamTicket mints a single-use ticket for opening the SSE stream, so the
// durable token never appears in the stream URL.
export function createStreamTicket(): Promise<{ ticket: string; expires_in: number }> {
  return scanRequest('POST', '/api/v1/scan/stream-ticket');
}

export function checkSession(): Promise<ScanSessionState> {
  return scanRequest<ScanSessionState>('GET', '/api/v1/scan/session');
}

export function searchParts(q: string): Promise<{ parts: ScanPart[] }> {
  return scanRequest('GET', `/api/v1/scan/parts?q=${encodeURIComponent(q)}`);
}

export function getPartDetail(partId: string): Promise<PartDetailResponse> {
  return scanRequest('GET', `/api/v1/scan/parts/${partId}`);
}

export function lookupBag(barcode: string): Promise<BagLookupResponse> {
  return scanRequest('POST', '/api/v1/scan/lookup', { barcode });
}

export function importScanPart(
  body: { distributor_id: string; sku: string; audit_log_id?: string | null },
): Promise<{ part_id: string }> {
  return scanRequest('POST', '/api/v1/scan/parts/import', body);
}

export interface ImportMultiResult {
  part_id: string;
  failed: Array<{ distributor_id: string; sku: string; reason: string }>;
}

export function importScanPartMulti(
  body: { listings: Array<{ distributor_id: string; sku: string }>; audit_log_id?: string | null },
): Promise<ImportMultiResult> {
  return scanRequest('POST', '/api/v1/scan/parts/import-multi', body);
}

export function stockAdjustment(
  body: { inventory_entry_id: string; delta: number; reason?: string; expected_quantity?: number },
): Promise<StockAdjustmentResponse> {
  return scanRequest('POST', '/api/v1/scan/stock-adjustment', body);
}

export function listOrders(): Promise<{ orders: ScanOrder[] }> {
  return scanRequest('GET', '/api/v1/scan/orders');
}

export function scanBarcode(
  body: { order_id: string; barcode: string },
): Promise<ScanResponse> {
  return scanRequest('POST', '/api/v1/scan/scans', body);
}

export function confirmReceipt(qty: number, lineItemId?: string): Promise<ConfirmReceiptResponse> {
  return scanRequest('POST', '/api/v1/scan/confirm', { qty, line_item_id: lineItemId });
}

export function skipItem(): Promise<void> {
  return scanRequest<void>('POST', '/api/v1/scan/skip');
}

export function searchDistributor(
  distributorId: string,
  q: string,
): Promise<{ items: DistributorSearchItem[] }> {
  return scanRequest(
    'GET',
    `/api/v1/scan/distributor-search?distributor_id=${encodeURIComponent(distributorId)}&q=${encodeURIComponent(q)}`,
  );
}

export function listScanStorageLocations(): Promise<{ locations: StorageLocation[] }> {
  return scanRequest('GET', '/api/v1/scan/storage-locations');
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
  locationId: string,
): Promise<StorageInventoryEntry[]> {
  return scanRequest('GET', `/api/v1/scan/storage/${locationId}/inventory`);
}

export function patchInventoryEntry(
  inventoryEntryId: string,
  storageLocation: PartLocation | null,
): Promise<{ id: string; storage_location: PartLocation | null }> {
  return scanRequest('PATCH', `/api/v1/scan/inventory-entries/${inventoryEntryId}`, {
    storage_location: storageLocation,
  });
}

// ── Scan-to-Focus ──────────────────────────────────────────────────────────────

export function scanFocus(
  entityType: 'part' | 'order' | 'storage' | 'build',
  entityId: string,
): Promise<void> {
  return scanRequest('POST', '/api/v1/scan/focus', {
    entity_type: entityType,
    entity_id: entityId,
  });
}

// ── Build Kitting ─────────────────────────────────────────────────────────────

export interface KittingProgress {
  pending: number;
  confirmed: number;
  shortfall: number;
  skipped: number;
}

export interface KittingSession {
  session: {
    id: string;
    build_id: string;
    build_ref: string;
    status: string;
  };
  progress: KittingProgress;
}

export interface KittingItem {
  part_id: string;
  part_mpn: string | null;
  part_manufacturer: string | null;
  part_description: string | null;
  quantity_needed: number;
  quantity_confirmed: number;
  status: 'pending' | 'confirmed' | 'shortfall' | 'skipped';
  storage_location_id: string | null;
  storage_location_path: string | null;
  storage_coordinate: number[] | null;
}

export function getKittingSession(kittingSessionId: string): Promise<KittingSession> {
  return scanRequest('GET', `/api/v1/scan/kitting/${kittingSessionId}`);
}

export function getKittingItems(kittingSessionId: string): Promise<{ items: KittingItem[] }> {
  return scanRequest('GET', `/api/v1/scan/kitting/${kittingSessionId}/items`);
}

export function kittingConfirm(
  kittingSessionId: string,
  partId: string,
  quantityConfirmed: number,
): Promise<{ item: unknown; progress: KittingProgress }> {
  return scanRequest('POST', `/api/v1/scan/kitting/${kittingSessionId}/confirm`, {
    part_id: partId,
    quantity_confirmed: quantityConfirmed,
  });
}

export function kittingShortfall(
  kittingSessionId: string,
  partId: string,
  quantityAvailable: number,
): Promise<{ item: unknown; progress: KittingProgress }> {
  return scanRequest('POST', `/api/v1/scan/kitting/${kittingSessionId}/shortfall`, {
    part_id: partId,
    quantity_available: quantityAvailable,
  });
}

export function kittingComplete(
  kittingSessionId: string,
): Promise<{ progress: KittingProgress }> {
  return scanRequest('POST', `/api/v1/scan/kitting/${kittingSessionId}/complete`);
}

export function kittingAbandon(kittingSessionId: string): Promise<void> {
  return scanRequest('POST', `/api/v1/scan/kitting/${kittingSessionId}/abandon`);
}

// ── Bulk scan (phone side) ─────────────────────────────────────────────────────

export interface StartBulkSessionResponse {
  session_id: string;
  item_count: number;
}

export interface AddBulkScanItemResponse {
  item_id: string;
  new: boolean;
  found_in_db: boolean;
  encoded_qty: number | null;
}

export function startBulkSession(): Promise<StartBulkSessionResponse> {
  return scanRequest('POST', '/api/v1/scan/bulk/start');
}

export function addBulkScanItem(
  sessionId: string,
  barcode: string,
): Promise<AddBulkScanItemResponse> {
  return scanRequest('POST', `/api/v1/scan/bulk/sessions/${sessionId}/items`, { barcode });
}

export function patchBulkScanItem(
  sessionId: string,
  itemId: string,
  body: { initial_qty: number | null },
): Promise<void> {
  return scanRequest('PATCH', `/api/v1/scan/bulk/sessions/${sessionId}/items/${itemId}`, body);
}

export function closeBulkSession(sessionId: string): Promise<void> {
  return scanRequest('POST', `/api/v1/scan/bulk/sessions/${sessionId}/close`);
}

export async function navigateBulkSession(sessionId: string): Promise<void> {
  const res = await fetch(`/api/v1/scan/bulk/sessions/${sessionId}/navigate`, {
    method: 'POST',
    credentials: 'same-origin', // auth rides the HttpOnly scan cookie
  });
  if (res.status === 401 || res.status === 410) throw new ScanExpiredError();
  if (res.status === 409) throw new Error('desktop_offline');
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
}

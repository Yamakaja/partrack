import { request, ApiError } from './client.js';

// ── Types ──────────────────────────────────────────────────────────────────────

export type BulkScanItemStatus = 'pending_review' | 'reviewed' | 'dismissed';

export type BulkScanTaskType =
  | 'unknown_part_import'
  | 'known_part_create_entry'
  | 'known_part_stock_adjustment'
  | 'unrecognized';

export interface BulkScanSessionCounts {
  pending: number;
  reviewed: number;
  dismissed: number;
  ready: number;
}

export interface BulkScanSession {
  id: string;
  created_at: string;
  closed_at: string | null;
  counts: BulkScanSessionCounts;
}

export interface BulkScanParsedFields {
  distributor?: string;
  distributor_id?: string;
  sku?: string;
  mpn?: string;
  manufacturer?: string;
  encoded_qty?: number;
}

export interface EnrichmentListing {
  sku: string;
  packaging_type: string;
  unit_price: string;
  stock_available: number | null;
  moq: number;
  fixed_order_cost?: string;
  price_breaks: Array<{ quantity: number; unit_price: string }>;
  listing_url?: string;
}

export interface EnrichmentResult {
  mpn: string;
  manufacturer: string;
  description: string;
  short_description: string;
  package: string;
  datasheet_url: string;
  product_url: string;
  image_url: string;
  lifecycle_status: string;
  listings: EnrichmentListing[];
}

export interface EnrichmentDistributorResult {
  distributor_id: string;
  name: string;
  results?: EnrichmentResult[];
  error?: string;
}

export interface EnrichmentDataAggregate {
  distributors: EnrichmentDistributorResult[];
}

export interface PartResolveItem {
  id: string;
  status: 'pending' | 'enriched' | 'failed' | 'committed';
  task_type: BulkScanTaskType | null;
  enrichment_data: EnrichmentDataAggregate | null;
  error_message: string | null;
  enriched_at: string | null;
}

export interface BulkScanItemPart {
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
}

export interface BulkScanItem {
  id: string;
  session_id: string;
  barcode: string;
  status: BulkScanItemStatus;
  task_type: BulkScanTaskType | null;
  parsed_fields: BulkScanParsedFields | null;
  initial_qty: number | null;
  part_id: string | null;
  part: BulkScanItemPart | null;
  resolve_item: PartResolveItem | null;
  created_at: string;
}

// ── API functions ──────────────────────────────────────────────────────────────

export function listBulkScanSessions(teamId: string): Promise<{ sessions: BulkScanSession[] }> {
  return request('GET', `/api/v1/teams/${teamId}/bulk-scan-sessions`);
}

export function getBulkScanBadgeCount(teamId: string): Promise<{ count: number }> {
  return request('GET', `/api/v1/teams/${teamId}/bulk-scan-badge`);
}

export function listBulkScanItems(
  teamId: string,
  sessionId: string,
): Promise<{ items: BulkScanItem[] }> {
  return request('GET', `/api/v1/teams/${teamId}/bulk-scan-sessions/${sessionId}/items`);
}

export async function markBulkScanItemReviewed(
  teamId: string,
  sessionId: string,
  itemId: string,
  partId?: string,
): Promise<void> {
  try {
    await request('PATCH', `/api/v1/teams/${teamId}/bulk-scan-sessions/${sessionId}/items/${itemId}`, {
      status: 'reviewed',
      ...(partId ? { part_id: partId } : {}),
    });
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) throw e;
    throw e;
  }
}

export function markBulkScanItemDismissed(
  teamId: string,
  sessionId: string,
  itemId: string,
): Promise<void> {
  return request('PATCH', `/api/v1/teams/${teamId}/bulk-scan-sessions/${sessionId}/items/${itemId}`, {
    status: 'dismissed',
  });
}

export function retryBulkScanItem(
  teamId: string,
  sessionId: string,
  itemId: string,
): Promise<{ job_id: string }> {
  return request('POST', `/api/v1/teams/${teamId}/bulk-scan-sessions/${sessionId}/items/${itemId}/retry`);
}

export function retryBulkScanItemDistributors(
  teamId: string,
  sessionId: string,
  itemId: string,
  distributorIds: string[],
): Promise<{ job_id: string }> {
  return request('POST', `/api/v1/teams/${teamId}/bulk-scan-sessions/${sessionId}/items/${itemId}/retry-distributors`, { distributor_ids: distributorIds });
}

export function deleteBulkScanSession(teamId: string, sessionId: string): Promise<void> {
  return request('DELETE', `/api/v1/teams/${teamId}/bulk-scan-sessions/${sessionId}`);
}

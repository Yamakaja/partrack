import { request } from './client.js';
import type { EnrichmentDataAggregate, BulkScanTaskType } from './bulkScan.js';

export interface PartResolveItemFull {
  id: string;
  status: 'pending' | 'enriched' | 'failed' | 'committed';
  task_type: BulkScanTaskType | null;
  source_type: string;
  source_id: string | null;
  resolved_part_id: string | null;
  enrichment_data: EnrichmentDataAggregate | null;
  error_message: string | null;
  enriched_at: string | null;
  committed_at: string | null;
}

export interface CommitResolveItemRequest {
  primary_distributor_id: string;
  primary_sku: string;
  additional_listings?: { distributor_id: string; sku: string }[];
  initial_qty?: number;
  initial_qty_reason?: string;
  storage_location_id?: string;
}

export function createPartResolveItem(
  teamId: string,
  body: { sku?: string; distributor_id?: string; mpn?: string; manufacturer?: string },
): Promise<{ resolve_item_id: string; job_id: string }> {
  return request('POST', `/api/v1/teams/${teamId}/parts/resolve`, body);
}

export function getPartResolveItem(teamId: string, resolveItemId: string): Promise<PartResolveItemFull> {
  return request('GET', `/api/v1/teams/${teamId}/parts/resolve/${resolveItemId}`);
}

export function commitResolveItem(
  teamId: string,
  resolveItemId: string,
  body: CommitResolveItemRequest,
): Promise<{ part_id: string }> {
  return request('POST', `/api/v1/teams/${teamId}/parts/resolve/${resolveItemId}/commit`, body);
}

export function retryResolveItemDistributors(
  teamId: string,
  resolveItemId: string,
  distributorIds: string[],
): Promise<{ job_id: string }> {
  return request('POST', `/api/v1/teams/${teamId}/parts/resolve/${resolveItemId}/retry-distributors`, {
    distributor_ids: distributorIds,
  });
}

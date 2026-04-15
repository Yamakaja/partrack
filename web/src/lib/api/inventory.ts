import { request, qs } from './client.js';
import type { InventoryEntry, StockAdjustment, ReplenishPreviewItem } from '$lib/types/inventory.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface ListInventoryParams {
  q?: string;
  storage_location_id?: string;
  below_min_stock?: boolean;
  limit?: number;
  offset?: number;
}

export function listInventory(teamId: string, params: ListInventoryParams = {}) {
  return request<PaginatedResponse<InventoryEntry>>('GET', `/api/v1/teams/${teamId}/inventory?${qs(params)}`);
}

export function createInventoryEntry(teamId: string, body: {
  part_id: string;
  storage_location_id?: string;
  storage_coordinate?: number[] | null;
  preferred_listing_id?: string;
  minimum_stock_quantity?: number;
  reorder_quantity?: number;
}) {
  return request<InventoryEntry>('POST', `/api/v1/teams/${teamId}/inventory`, body);
}

export function getInventoryEntry(teamId: string, entryId: string) {
  return request<InventoryEntry>('GET', `/api/v1/teams/${teamId}/inventory/${entryId}`);
}

export function updateInventoryEntry(teamId: string, entryId: string, body: {
  storage_location_id?: string | null;
  storage_coordinate?: number[] | null;
  preferred_listing_id?: string | null;
  minimum_stock_quantity?: number | null;
  reorder_quantity?: number | null;
}) {
  return request<InventoryEntry>('PATCH', `/api/v1/teams/${teamId}/inventory/${entryId}`, body);
}

export function listAdjustments(teamId: string, entryId: string, params: { limit?: number; offset?: number } = {}) {
  return request<PaginatedResponse<StockAdjustment>>(
    'GET', `/api/v1/teams/${teamId}/inventory/${entryId}/adjustments?${qs(params)}`,
  );
}

export function createAdjustment(teamId: string, entryId: string, body: { delta: number; reason: string }) {
  return request<StockAdjustment & { quantity_on_hand: number }>(
    'POST', `/api/v1/teams/${teamId}/inventory/${entryId}/adjustments`, body,
  );
}

export function replenishPreview(teamId: string) {
  return request<{ items: ReplenishPreviewItem[] }>('GET', `/api/v1/teams/${teamId}/inventory/replenish-preview`);
}

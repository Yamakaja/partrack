import { request } from './client.js';
import type { StorageLocation, GridConfig, InventoryEntry } from '$lib/types/inventory.js';

export function listStorageLocations(teamId: string) {
  return request<{ items: StorageLocation[] }>('GET', `/api/v1/teams/${teamId}/storage-locations`);
}

export function getStorageLocation(teamId: string, locationId: string) {
  return request<{ location: StorageLocation; ancestors: StorageLocation[] }>(
    'GET',
    `/api/v1/teams/${teamId}/storage-locations/${locationId}`,
  );
}

export function listInventoryByLocation(
  teamId: string,
  locationId: string,
  params?: { limit?: number; offset?: number },
) {
  const qs = new URLSearchParams();
  if (params?.limit != null) qs.set('limit', String(params.limit));
  if (params?.offset != null) qs.set('offset', String(params.offset));
  const query = qs.toString() ? `?${qs}` : '';
  return request<{ items: InventoryEntry[]; total: number }>(
    'GET',
    `/api/v1/teams/${teamId}/storage-locations/${locationId}/inventory${query}`,
  );
}

export function createStorageLocation(
  teamId: string,
  body: { label: string; description?: string; parent_id?: string; grid_config?: GridConfig | null },
) {
  return request<StorageLocation>('POST', `/api/v1/teams/${teamId}/storage-locations`, body);
}

export function updateStorageLocation(
  teamId: string,
  locationId: string,
  body: { label?: string; description?: string; grid_config?: GridConfig | null },
) {
  return request<StorageLocation>(
    'PATCH',
    `/api/v1/teams/${teamId}/storage-locations/${locationId}`,
    body,
  );
}

export function deleteStorageLocation(teamId: string, locationId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/storage-locations/${locationId}`);
}

import { request } from './client.js';
import type { StorageLocation } from '$lib/types/inventory.js';

export function listStorageLocations(teamId: string) {
  return request<{ items: StorageLocation[] }>('GET', `/api/v1/teams/${teamId}/storage-locations`);
}

export function createStorageLocation(teamId: string, label: string, description?: string) {
  return request<StorageLocation>('POST', `/api/v1/teams/${teamId}/storage-locations`, { label, description });
}

export function updateStorageLocation(teamId: string, locationId: string, body: { label?: string; description?: string }) {
  return request<StorageLocation>('PATCH', `/api/v1/teams/${teamId}/storage-locations/${locationId}`, body);
}

export function deleteStorageLocation(teamId: string, locationId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/storage-locations/${locationId}`);
}

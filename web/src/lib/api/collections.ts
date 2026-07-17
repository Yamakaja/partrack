import { request } from './client.js';

// Part collections ("Lists"): user-private bags of parts within a team.

export interface PartCollectionSummary {
  id: string;
  name: string;
  archived: boolean;
  collect_capture_qty: boolean | null;
  created_at: string;
  updated_at: string;
  item_count: number;
  total_quantity: number;
  unquantified_count: number;
}

export interface CollectionItem {
  id: string;
  part_id: string;
  quantity: number | null;
  note: string | null;
  mpn: string | null;
  manufacturer: string | null;
  description: string;
  lifecycle_status: string;
  image_url: string | null;
}

export interface CollectionDetail {
  id: string;
  name: string;
  archived: boolean;
  collect_capture_qty: boolean | null;
  created_at: string;
  updated_at: string;
  items: CollectionItem[];
}

export function listCollections(teamId: string, includeArchived = false) {
  const suffix = includeArchived ? '?archived=true' : '';
  return request<{ collections: PartCollectionSummary[] }>(
    'GET', `/api/v1/teams/${teamId}/collections${suffix}`,
  );
}

export function createCollection(teamId: string, name = '') {
  return request<PartCollectionSummary>('POST', `/api/v1/teams/${teamId}/collections`, { name });
}

export function getCollection(teamId: string, collectionId: string) {
  return request<CollectionDetail>('GET', `/api/v1/teams/${teamId}/collections/${collectionId}`);
}

export function updateCollection(
  teamId: string,
  collectionId: string,
  body: { name?: string; archived?: boolean; collect_capture_qty?: boolean },
) {
  return request<PartCollectionSummary>('PATCH', `/api/v1/teams/${teamId}/collections/${collectionId}`, body);
}

export function deleteCollection(teamId: string, collectionId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/collections/${collectionId}`);
}

export function addCollectionItem(
  teamId: string,
  collectionId: string,
  body: { part_id: string; quantity?: number | null; note?: string | null },
) {
  return request<{ id: string; part_id: string; quantity: number | null; note: string | null }>(
    'POST', `/api/v1/teams/${teamId}/collections/${collectionId}/items`, body,
  );
}

export function bulkAddCollectionItems(
  teamId: string,
  collectionId: string,
  items: { part_id: string; quantity?: number | null; note?: string | null }[],
) {
  return request<{ added: number }>(
    'POST', `/api/v1/teams/${teamId}/collections/${collectionId}/items/bulk`, { items },
  );
}

// quantity/note use set-if-present semantics: pass null to clear, omit to leave.
export function updateCollectionItem(
  teamId: string,
  collectionId: string,
  itemId: string,
  body: { quantity?: number | null; note?: string | null },
) {
  return request<{ id: string; part_id: string; quantity: number | null; note: string | null }>(
    'PATCH', `/api/v1/teams/${teamId}/collections/${collectionId}/items/${itemId}`, body,
  );
}

export function removeCollectionItem(teamId: string, collectionId: string, itemId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/collections/${collectionId}/items/${itemId}`);
}

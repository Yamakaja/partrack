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
  checked_count: number;
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
  /** Available (free) stock = on hand − reservations; null when not tracked. */
  available_qty: number | null;
  /**
   * Selection state — null when unchecked. Persisted rather than held in the
   * client, because the phone's pick mode writes it from another device.
   */
  checked_at: string | null;
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

export interface CollectionItemEcho {
  id: string;
  part_id: string;
  quantity: number | null;
  note: string | null;
  checked_at: string | null;
}

// quantity/note use set-if-present semantics: pass null to clear, omit to leave.
// `checked` is a plain bool — the selection has no third state.
export function updateCollectionItem(
  teamId: string,
  collectionId: string,
  itemId: string,
  body: { quantity?: number | null; note?: string | null; checked?: boolean },
) {
  return request<CollectionItemEcho>(
    'PATCH', `/api/v1/teams/${teamId}/collections/${collectionId}/items/${itemId}`, body,
  );
}

/**
 * The list's selection ops. 'all' | 'none' | 'invert' sweep the whole list;
 * 'set' writes `checked` to `item_ids` only (row toggles, shift-click ranges).
 */
export function setCollectionItemsChecked(
  teamId: string,
  collectionId: string,
  body: { op: 'all' | 'none' | 'invert' | 'set'; item_ids?: string[]; checked?: boolean },
) {
  return request<{ item_count: number; checked_count: number }>(
    'POST', `/api/v1/teams/${teamId}/collections/${collectionId}/items/checked`, body,
  );
}

export function removeCollectionItem(teamId: string, collectionId: string, itemId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/collections/${collectionId}/items/${itemId}`);
}

// Omit itemIds to empty the list; pass them to delete just those rows.
export function clearCollectionItems(teamId: string, collectionId: string, itemIds?: string[]) {
  return request<{ removed: number }>(
    'DELETE', `/api/v1/teams/${teamId}/collections/${collectionId}/items`,
    itemIds ? { item_ids: itemIds } : undefined,
  );
}

// Adds every part matching the current parts-search filters (across all pages)
// to the collection as bare membership. `query` is the same serialized filter
// string the parts table builds for its bulk refresh actions.
export function addSearchResultsToCollection(teamId: string, collectionId: string, query: string) {
  return request<{ added: number; matched: number }>(
    'POST',
    `/api/v1/teams/${teamId}/collections/${collectionId}/items/from-search${query ? `?${query}` : ''}`,
  );
}

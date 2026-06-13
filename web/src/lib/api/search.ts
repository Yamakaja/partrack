import { request } from './client.js';

export interface PartSearchResult {
  id: string;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  short_description: string | null;
  lifecycle_status: string;
}

export interface ProjectSearchResult {
  id: string;
  name: string;
  description: string | null;
  status: string;
}

export interface RevisionSearchResult {
  id: string;
  label: string;
  project_id: string;
  project_name: string;
}

export interface OrderSearchResult {
  id: string;
  status: string;
  purchase_order_number: string | null;
  external_reference: string | null;
  distributor_name: string;
}

export interface BuildSearchResult {
  id: string;
  status: string;
  quantity: number;
  project_name: string;
  revision_label: string;
}

export interface StorageSearchResult {
  id: string;
  label: string;
  description: string | null;
  ancestor_path: string | null;
}

export interface SearchResults {
  parts: PartSearchResult[];
  projects: ProjectSearchResult[];
  revisions: RevisionSearchResult[];
  orders: OrderSearchResult[];
  builds: BuildSearchResult[];
  storage_locations: StorageSearchResult[];
}

export function globalSearch(teamId: string, q: string) {
  return request<SearchResults>(
    'GET',
    `/api/v1/teams/${teamId}/search?q=${encodeURIComponent(q)}`,
  );
}

import { request } from './client.js';
import type { Distributor, DistributorConfig } from '$lib/types/parts.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface DistributorSearchProduct {
  mpn: string;
  manufacturer: string;
  description: string;
  category: string;
  primary_sku: string;
  package: string;
  unit_price: string;
  stock_available: number | null;
}

export interface DistributorSearchResult {
  items: DistributorSearchProduct[];
  total: number;
  exact_match_indices: number[];
}

export function listDistributors() {
  return request<PaginatedResponse<Distributor>>('GET', '/api/v1/distributors');
}

export function listDistributorConfigs(teamId: string) {
  return request<{ items: DistributorConfig[] }>('GET', `/api/v1/teams/${teamId}/distributor-configs`);
}

export function upsertDistributorConfig(
  teamId: string,
  distributorId: string,
  body: {
    enabled: boolean;
    preferred: boolean;
    cache_duration_seconds: number;
    api_credentials?: Record<string, unknown> | null;
  },
) {
  return request<DistributorConfig>(
    'PUT', `/api/v1/teams/${teamId}/distributor-configs/${distributorId}`, body,
  );
}

export function deleteDistributorConfig(teamId: string, distributorId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/distributor-configs/${distributorId}`);
}

export function searchDistributor(
  teamId: string,
  distributorId: string,
  keywords: string,
  limit = 20,
) {
  const q = new URLSearchParams({ keywords, limit: String(limit) });
  return request<DistributorSearchResult>(
    'GET',
    `/api/v1/teams/${teamId}/distributor-configs/${distributorId}/search?${q}`,
  );
}

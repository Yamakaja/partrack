import { request, qs } from './client.js';
import type { Part, PartSummary, Parameter, Listing, CategoryParameter } from '$lib/types/parts.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface ParamFilter {
  type: 'numeric' | 'enum';
  min?: number;
  max?: number;
  values?: string[];
}

export interface ListPartsParams {
  team_id?: string;
  q?: string;
  mpn?: string;
  category?: string;
  category_id?: number;
  package?: string;
  lifecycle_status?: string[];
  scope?: string;
  stock_status?: string[];
  param_filters?: Record<number, ParamFilter>;
  limit?: number;
  offset?: number;
}

export function listParts({ param_filters, ...rest }: ListPartsParams = {}) {
  const p = new URLSearchParams(qs(rest));
  if (param_filters) {
    for (const [id, f] of Object.entries(param_filters)) {
      if (f.type === 'numeric') {
        if (f.min !== undefined) p.set(`param_${id}_min`, String(f.min));
        if (f.max !== undefined) p.set(`param_${id}_max`, String(f.max));
      } else if (f.type === 'enum' && f.values?.length) {
        for (const v of f.values) p.append(`param_${id}_values`, v);
      }
    }
  }
  return request<PaginatedResponse<PartSummary>>('GET', `/api/v1/parts?${p.toString()}`);
}

export function getCategoryParameters(categoryId: number, teamId?: string) {
  const q = teamId ? `?team_id=${teamId}` : '';
  return request<CategoryParameter[]>('GET', `/api/v1/categories/${categoryId}/parameters${q}`);
}

export function getPart(partId: string, teamId?: string) {
  const q = teamId ? `?team_id=${teamId}` : '';
  return request<Part>('GET', `/api/v1/parts/${partId}${q}`);
}

export function createPart(body: Partial<Part>) {
  return request<Part>('POST', '/api/v1/parts', body);
}

export function updatePart(partId: string, body: Partial<Part>) {
  return request<Part>('PATCH', `/api/v1/parts/${partId}`, body);
}

export function enrichPart(partId: string, teamId: string, body: { sku: string; distributor_id: string }) {
  return request<{ job_id: string }>('POST', `/api/v1/teams/${teamId}/parts/${partId}/enrich`, body);
}

export function importPart(teamId: string, body: { sku: string; distributor_id: string }) {
  return request<{ part_id: string }>('POST', `/api/v1/teams/${teamId}/parts/import`, body);
}

export function addListing(partId: string, teamId: string, body: { sku: string; distributor_id: string }) {
  return request<{ job_id: string }>('POST', `/api/v1/teams/${teamId}/parts/${partId}/listings`, body);
}

export function deleteListing(partId: string, listingId: string, teamId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/parts/${partId}/listings/${listingId}`);
}

export function listParameters(partId: string) {
  return request<{ user_specified: Parameter[]; distributor_sourced: Parameter[] }>(
    'GET', `/api/v1/parts/${partId}/parameters`,
  );
}

export function createParameter(partId: string, body: { name: string; value: string; unit?: string }) {
  return request<Parameter>('POST', `/api/v1/parts/${partId}/parameters`, body);
}

export function deleteParameter(partId: string, parameterId: string) {
  return request<void>('DELETE', `/api/v1/parts/${partId}/parameters/${parameterId}`);
}

export function listListings(partId: string) {
  return request<{ items: Listing[] }>('GET', `/api/v1/parts/${partId}/listings`);
}

export function refreshListing(partId: string, listingId: string, teamId: string) {
  return request<{ job_id: string }>('POST', `/api/v1/teams/${teamId}/parts/${partId}/listings/${listingId}/refresh`);
}

export function refreshAllListings(partId: string, teamId: string) {
  return request<{ job_ids: string[] }>('POST', `/api/v1/teams/${teamId}/parts/${partId}/listings/refresh`);
}

export function reenrichAllParts(teamId: string) {
  return request<{ job_count: number }>('POST', `/api/v1/teams/${teamId}/parts/enrich-all`);
}

export interface PartAllocation {
  build_id: string;
  project_id: string;
  project_name: string;
  build_status: string;
  build_quantity: number;
  quantity_needed: number;
  quantity_reserved: number;
  quantity_received: number;
}

export function getPartAllocations(partId: string, teamId: string) {
  return request<{ items: PartAllocation[] }>('GET', `/api/v1/teams/${teamId}/parts/${partId}/allocations`);
}

import { request, qs } from './client.js';
import type { Part, PartSummary, Parameter, Listing } from '$lib/types/parts.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface ListPartsParams {
  team_id?: string;
  q?: string;
  mpn?: string;
  category?: string;
  package?: string;
  lifecycle_status?: string;
  scope?: string;
  stock_status?: string;
  limit?: number;
  offset?: number;
}

export function listParts(params: ListPartsParams = {}) {
  return request<PaginatedResponse<PartSummary>>('GET', `/api/v1/parts?${qs(params)}`);
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

export function enrichPart(partId: string, teamId: string) {
  return request<{ job_id: string }>('POST', `/api/v1/parts/${partId}/enrich?team_id=${teamId}`);
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

export function refreshListing(partId: string, listingId: string, teamId?: string) {
  const q = teamId ? `?team_id=${teamId}` : '';
  return request<{ job_id: string }>('POST', `/api/v1/parts/${partId}/listings/${listingId}/refresh${q}`);
}

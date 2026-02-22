import { request, qs } from './client.js';
import type { GeneralCheckout } from '$lib/types/orders.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export function listCheckouts(teamId: string, params: { limit?: number; offset?: number } = {}) {
  return request<PaginatedResponse<GeneralCheckout>>('GET', `/api/v1/teams/${teamId}/checkouts?${qs(params)}`);
}

export function createCheckout(teamId: string, body: {
  notes?: string;
  items: { part_id: string; quantity: number; inventory_entry_id: string }[];
}) {
  return request<GeneralCheckout>('POST', `/api/v1/teams/${teamId}/checkouts`, body);
}

export function getCheckout(teamId: string, checkoutId: string) {
  return request<GeneralCheckout>('GET', `/api/v1/teams/${teamId}/checkouts/${checkoutId}`);
}

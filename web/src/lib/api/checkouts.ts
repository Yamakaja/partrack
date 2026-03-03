import { request, qs } from './client.js';
import type { GeneralCheckout } from '$lib/types/orders.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export function listCheckouts(teamId: string, params: { limit?: number; offset?: number } = {}) {
  return request<PaginatedResponse<GeneralCheckout>>('GET', `/api/v1/teams/${teamId}/checkouts?${qs(params)}`);
}

export interface InsufficientCheckoutItem {
  inventory_entry_id: string;
  part_id: string;
  part_mpn: string | null;
  quantity_on_hand: number;
  quantity_requested: number;
}

export function createCheckout(teamId: string, body: {
  reason?: string;
  items: { inventory_entry_id: string; quantity: number; reconcile_to?: number }[];
}) {
  return request<GeneralCheckout>('POST', `/api/v1/teams/${teamId}/checkouts`, body);
}

export function getCheckout(teamId: string, checkoutId: string) {
  return request<GeneralCheckout>('GET', `/api/v1/teams/${teamId}/checkouts/${checkoutId}`);
}

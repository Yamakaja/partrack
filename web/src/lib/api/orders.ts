import { request, qs } from './client.js';
import type { Order, OrderLineItem } from '$lib/types/orders.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export function listOrders(teamId: string, params: { status?: string; limit?: number; offset?: number } = {}) {
  return request<PaginatedResponse<Order>>('GET', `/api/v1/teams/${teamId}/orders?${qs(params)}`);
}

export function getOrder(teamId: string, orderId: string) {
  return request<Order & { items: OrderLineItem[] }>('GET', `/api/v1/teams/${teamId}/orders/${orderId}`);
}

export function updateOrderLineItem(teamId: string, orderId: string, itemId: string, body: { quantity?: number }) {
  return request<OrderLineItem>('PATCH', `/api/v1/teams/${teamId}/orders/${orderId}/items/${itemId}`, body);
}

export function removeOrderLineItem(teamId: string, orderId: string, itemId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/orders/${orderId}/items/${itemId}`);
}

export function submitOrder(teamId: string, orderId: string) {
  return request<Order>('POST', `/api/v1/teams/${teamId}/orders/${orderId}/submit`);
}

export function markOrderPlaced(teamId: string, orderId: string, distributor_order_ref: string) {
  return request<Order>('POST', `/api/v1/teams/${teamId}/orders/${orderId}/mark-placed`, { distributor_order_ref });
}

export function receiveLineItem(teamId: string, orderId: string, itemId: string, quantity_received: number) {
  return request<OrderLineItem & { quantity_on_hand: number }>(
    'POST', `/api/v1/teams/${teamId}/orders/${orderId}/items/${itemId}/receive`,
    { quantity_received },
  );
}

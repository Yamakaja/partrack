import { request, qs } from './client.js';
import type { Order, OrderLineItem, QuoteItem, LinkedBuild } from '$lib/types/orders.js';
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

export function markOrderPlaced(teamId: string, orderId: string, external_reference: string) {
  return request<Order>('POST', `/api/v1/teams/${teamId}/orders/${orderId}/reference`, { external_reference });
}

export function receiveLineItem(teamId: string, orderId: string, itemId: string, quantity_received: number) {
  return request<{ line_item_id: string; quantity_ordered: number; quantity_received: number; order_status: string; new_quantity_on_hand: number }>(
    'POST', `/api/v1/teams/${teamId}/orders/${orderId}/items/${itemId}/receive`,
    { quantity_received },
  );
}

export function updateOrderPurchaseOrderNumber(teamId: string, orderId: string, purchase_order_number: string | null) {
  return request<void>('PATCH', `/api/v1/teams/${teamId}/orders/${orderId}`, { purchase_order_number });
}

export function updateOrderNotes(teamId: string, orderId: string, notes: string | null) {
  return request<void>('PATCH', `/api/v1/teams/${teamId}/orders/${orderId}`, { notes });
}

export function cancelOrder(teamId: string, orderId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/orders/${orderId}`);
}

export function pushToDistributor(teamId: string, orderId: string) {
  return request<{ list_id: string; list_url: string }>(
    'POST', `/api/v1/teams/${teamId}/orders/${orderId}/push-to-distributor`,
  );
}

export function requestQuote(teamId: string, orderId: string) {
  return request<{ quote_id: number; quote_name: string; items: QuoteItem[] }>(
    'POST', `/api/v1/teams/${teamId}/orders/${orderId}/request-quote`,
  );
}

export function getOrderLinkedBuilds(teamId: string, orderId: string) {
  return request<LinkedBuild[]>('GET', `/api/v1/teams/${teamId}/orders/${orderId}/linked-builds`);
}

export function getDistributorStatus(teamId: string, orderId: string) {
  return request<{ status: string; status_detail: string; last_checked_at: string }>(
    'GET', `/api/v1/teams/${teamId}/orders/${orderId}/distributor-status`,
  );
}

export function retryUnresolvedItems(teamId: string, orderId: string) {
  return request<{ job_id: string }>(
    'POST',
    `/api/v1/teams/${teamId}/orders/${orderId}/retry-unresolved`,
  );
}

export interface ImportOrderProgress {
  status: 'progress' | 'complete' | 'error';
  job_id: string;
  order_id?: string;
  total: number;
  resolved?: number;
  processed?: number;
  attempted?: number;
  unresolved_skus: string[];
  extra_items_added?: number;
  error?: string;
}

export function importOrder(
  teamId: string,
  body: { sales_order_id: string; existing_order_id?: string; force?: boolean },
) {
  return request<{ job_id: string }>('POST', `/api/v1/teams/${teamId}/orders/import`, body);
}

export function commitLinkOrder(
  teamId: string,
  orderId: string,
  token: string,
  buildPriorities: { build_id: string; keep: boolean }[],
) {
  return request<{ job_id: string }>(
    'POST',
    `/api/v1/teams/${teamId}/orders/${orderId}/link-commit`,
    { token, build_priorities: buildPriorities },
  );
}

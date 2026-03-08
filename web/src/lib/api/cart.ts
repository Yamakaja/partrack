import { request } from './client.js';
import type { Cart, CartItem } from '$lib/types/orders.js';

export function getCart(teamId: string) {
  return request<Cart>('GET', `/api/v1/teams/${teamId}/cart`);
}

export function getCartCount(teamId: string) {
  return request<{ count: number }>('GET', `/api/v1/teams/${teamId}/cart/count`);
}

export function addCartItem(teamId: string, body: { part_id: string; quantity: number; listing_id?: string }) {
  return request<CartItem>('POST', `/api/v1/teams/${teamId}/cart/items`, body);
}

export function updateCartItem(teamId: string, itemId: string, body: { quantity?: number; listing_id?: string | null }) {
  return request<CartItem>('PATCH', `/api/v1/teams/${teamId}/cart/items/${itemId}`, body);
}

export function removeCartItem(teamId: string, itemId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/cart/items/${itemId}`);
}

export function addLowStock(teamId: string) {
  return request<{ added_count: number }>('POST', `/api/v1/teams/${teamId}/cart/add-low-stock`);
}

export function convertCart(teamId: string) {
  return request<{ order_ids: string[] }>('POST', `/api/v1/teams/${teamId}/cart/convert`);
}

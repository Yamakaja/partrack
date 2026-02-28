/**
 * SSE-driven cache invalidation.
 *
 * When another user mutates shared data the server publishes an entity-change
 * event over the team's SSE channel (the acting user's own channel is skipped
 * server-side, so we never receive our own events here).  This module
 * subscribes to those events and calls invalidateQueries so that every
 * connected browser stays up-to-date without manual refresh.
 */
import type { QueryClient } from '@tanstack/svelte-query';
import { subscribeSSEType } from '$lib/api/sse.js';

let qc: QueryClient | null = null;
let unsubscribers: Array<() => void> = [];

export function initCacheInvalidation(client: QueryClient) {
  qc = client;
}

export function activateTeamInvalidation(teamId: string) {
  deactivateTeamInvalidation();

  if (!qc) return;

  unsubscribers = [
    subscribeSSEType(teamId, 'part_changed', (ev) => {
      const partId = ev.data.part_id as string | undefined;
      if (partId) qc!.invalidateQueries({ queryKey: ['part', partId, teamId] });
      qc!.invalidateQueries({ queryKey: ['parts', teamId] });
    }),

    subscribeSSEType(teamId, 'inventory_changed', (ev) => {
      const partId = ev.data.part_id as string | undefined;
      const entryId = ev.data.entry_id as string | undefined;
      if (partId) qc!.invalidateQueries({ queryKey: ['part', partId, teamId] });
      qc!.invalidateQueries({ queryKey: ['parts', teamId] });
      qc!.invalidateQueries({ queryKey: ['inventory', teamId] });
      if (entryId) {
        qc!.invalidateQueries({ queryKey: ['inventory-entry', teamId, entryId] });
        qc!.invalidateQueries({ queryKey: ['adjustments', teamId, entryId] });
      }
    }),

    subscribeSSEType(teamId, 'cart_changed', () => {
      qc!.invalidateQueries({ queryKey: ['cart', teamId] });
    }),

    subscribeSSEType(teamId, 'order_changed', (ev) => {
      qc!.invalidateQueries({ queryKey: ['orders', teamId] });
      const orderId = ev.data.order_id as string | undefined;
      if (orderId) qc!.invalidateQueries({ queryKey: ['order', orderId] });
    }),

    subscribeSSEType(teamId, 'storage_changed', () => {
      qc!.invalidateQueries({ queryKey: ['storage', teamId] });
    }),

    subscribeSSEType(teamId, 'project_changed', (ev) => {
      qc!.invalidateQueries({ queryKey: ['projects', teamId] });
      const projectId = ev.data.project_id as string | undefined;
      if (projectId) qc!.invalidateQueries({ queryKey: ['project', teamId, projectId] });
    }),
  ];
}

export function deactivateTeamInvalidation() {
  unsubscribers.forEach((unsub) => unsub());
  unsubscribers = [];
}

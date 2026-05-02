/**
 * SSE-driven cache invalidation.
 *
 * When a user mutates shared data the server publishes an entity-change event
 * over the team's SSE channel. This module subscribes to those events and
 * calls invalidateQueries so that every connected browser stays up-to-date
 * without manual refresh.
 */
import type { QueryClient } from '@tanstack/svelte-query';
import { subscribeSSEType } from '$lib/api/sse.js';
import { navigate } from '$lib/router/index.svelte.js';
import { pushToast } from '$lib/stores/toasts.svelte.js';
import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';

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
      if (projectId) {
        qc!.invalidateQueries({ queryKey: ['project', teamId, projectId] });
        qc!.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
      }
    }),

    subscribeSSEType(teamId, 'build_changed', (ev) => {
      qc!.invalidateQueries({ queryKey: ['builds', teamId] });
      qc!.invalidateQueries({ queryKey: ['dashboard', teamId] });
      qc!.invalidateQueries({ queryKey: ['order-builds'] });
      const buildId = ev.data.build_id as string | undefined;
      if (buildId) qc!.invalidateQueries({ queryKey: ['build', buildId] });
    }),

    subscribeSSEType(teamId, 'revision_changed', (ev) => {
      const projectId = ev.data.project_id as string | undefined;
      const revisionId = ev.data.revision_id as string | undefined;
      if (projectId) {
        qc!.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
        if (revisionId) {
          qc!.invalidateQueries({ queryKey: ['revision', teamId, projectId, revisionId] });
        }
      }
    }),

    subscribeSSEType(teamId, 'checkout_changed', () => {
      qc!.invalidateQueries({ queryKey: ['checkouts', teamId] });
    }),

    subscribeSSEType(teamId, 'scan_audit_changed', () => {
      qc!.invalidateQueries({ queryKey: ['scan-audit-log', teamId] });
    }),

    subscribeSSEType(teamId, 'build_kitting_changed', (ev) => {
      const kittingSessionId = ev.data.kitting_session_id as string | undefined;
      const buildId = ev.data.build_id as string | undefined;
      if (kittingSessionId) qc!.invalidateQueries({ queryKey: ['kitting', teamId, kittingSessionId] });
      if (buildId) qc!.invalidateQueries({ queryKey: ['build', buildId] });
      qc!.invalidateQueries({ queryKey: ['builds', teamId] });
    }),

    subscribeSSEType(teamId, 'bulk_scan_changed', (ev) => {
      const sessionId = ev.data.session_id as string | undefined;
      qc!.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
      if (sessionId) qc!.invalidateQueries({ queryKey: ['bulk-scan-items', teamId, sessionId] });
    }),

    subscribeSSEType(teamId, 'part_resolve_changed', () => {
      qc!.invalidateQueries({ queryKey: ['bulk-scan-items', teamId] });
      qc!.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
    }),

    subscribeSSEType(teamId, 'scan_focus_requested', (ev) => {
      const entityType = ev.data.entity_type as string | undefined;
      const entityId = ev.data.entity_id as string | undefined;
      if (!entityType || !entityId) return;

      const tid = activeTeamStore.current?.teamId;
      if (!tid) return;

      let path = '';
      switch (entityType) {
        case 'part':
          path = `/teams/${tid}/parts/${entityId}`;
          break;
        case 'order':
          path = `/teams/${tid}/orders/${entityId}`;
          break;
        case 'storage':
          path = `/teams/${tid}/storage/${entityId}`;
          break;
        case 'build':
          path = `/teams/${tid}/builds/${entityId}`;
          break;
        case 'bulk_scan_session':
          path = `/teams/${tid}/bulk-scan/${entityId}`;
          break;
      }
      if (path) {
        navigate(path);
        pushToast('info', 'Scanner: navigated to ' + entityType);
      }
    }),
  ];
}

export function deactivateTeamInvalidation() {
  unsubscribers.forEach((unsub) => unsub());
  unsubscribers = [];
}

import {
  sseConnectionStore,
  markSSEConnected,
  markSSEReconnecting,
  markSSEReconnected,
  markSSEDisconnected,
  resetSSEConnection,
} from '$lib/stores/sseConnection.svelte.js';
import { pushToast } from '$lib/stores/toasts.svelte.js';

export interface SseEvent {
  type: string;
  job_id: string;
  data: Record<string, unknown>;
}

type SseCallback = (event: SseEvent) => void;

let currentTeamId: string | null = null;
let es: EventSource | null = null;
let hadSuccessfulOpen = false;
let lastMessageAt = 0;
let disconnectTimer: ReturnType<typeof setTimeout> | null = null;
let watchdogInterval: ReturnType<typeof setInterval> | null = null;
let onReconnect: (() => void) | null = null;

const subscribers = new Map<string, SseCallback[]>(); // keyed by job_id
const typeSubscribers = new Map<string, SseCallback[]>(); // keyed by event type

export function setReconnectHandler(cb: () => void): void {
  onReconnect = cb;
}

export function openForTeam(teamId: string) {
  if (currentTeamId === teamId && es && es.readyState !== EventSource.CLOSED) return;
  closeSSE();
  currentTeamId = teamId;
  hadSuccessfulOpen = false;
  sseConnectionStore.current = { state: 'connecting', lastConnected: null, reconnectCount: 0 };
  attachEventSource(teamId);
}

// Recreates the EventSource for an existing session without resetting hadSuccessfulOpen
// or subscriber maps. Used when the browser closes the connection (e.g. on a 502).
function restartEventSource(teamId: string) {
  if (watchdogInterval !== null) {
    clearInterval(watchdogInterval);
    watchdogInterval = null;
  }
  es?.close();
  es = null;
  attachEventSource(teamId);
}

function attachEventSource(teamId: string) {
  es = new EventSource(`/api/v1/teams/${teamId}/events`, { withCredentials: true });

  es.onopen = () => {
    clearDisconnectTimer();
    lastMessageAt = Date.now();
    if (!hadSuccessfulOpen) {
      hadSuccessfulOpen = true;
      markSSEConnected();
    } else {
      markSSEReconnected();
      onReconnect?.();
      pushToast('success', 'Connection restored — data refreshed');
    }
  };

  es.onmessage = (e) => {
    lastMessageAt = Date.now();
    try {
      const event: SseEvent = JSON.parse(e.data);
      if (event.type === 'ping') return; // infrastructure keepalive, silently swallow
      const cbs = subscribers.get(event.job_id) ?? [];
      cbs.forEach((cb) => cb(event));
      const typeCbs = typeSubscribers.get(event.type) ?? [];
      typeCbs.forEach((cb) => cb(event));
    } catch {
      /* ignore malformed */
    }
  };

  es.onerror = () => {
    const state = sseConnectionStore.current.state;

    if (es?.readyState === EventSource.CLOSED) {
      // Browser received a non-2xx response (e.g. 502 from proxy) and will not
      // retry automatically. Update state and schedule a manual reconnect.
      if (hadSuccessfulOpen && state !== 'disconnected' && disconnectTimer === null) {
        markSSEReconnecting();
        disconnectTimer = setTimeout(() => {
          disconnectTimer = null;
          markSSEDisconnected();
        }, 30_000);
      }
      const tid = currentTeamId;
      if (tid) {
        setTimeout(() => {
          if (currentTeamId === tid) restartEventSource(tid);
        }, 3_000);
      }
      return;
    }

    // readyState === CONNECTING: browser is auto-retrying after a TCP-level drop.
    if (hadSuccessfulOpen && disconnectTimer === null && state !== 'disconnected') {
      markSSEReconnecting();
      disconnectTimer = setTimeout(() => {
        disconnectTimer = null;
        markSSEDisconnected();
      }, 30_000);
    }
    // If already disconnected: stay there until onopen fires.
  };

  // Watchdog: fire every 15s. If no message received in 30s (3 missed 10s pings),
  // the TCP connection may be silently stuck. Proactively mark as reconnecting.
  watchdogInterval = setInterval(() => {
    const timeSinceLastMessage = Date.now() - lastMessageAt;
    if (
      lastMessageAt > 0 &&
      timeSinceLastMessage > 30_000 &&
      sseConnectionStore.current.state === 'connected'
    ) {
      markSSEReconnecting();
      if (disconnectTimer === null) {
        disconnectTimer = setTimeout(() => {
          disconnectTimer = null;
          markSSEDisconnected();
        }, 30_000);
      }
    }
  }, 15_000);
}

function clearDisconnectTimer() {
  if (disconnectTimer !== null) {
    clearTimeout(disconnectTimer);
    disconnectTimer = null;
  }
}

export function subscribeSSE(
  teamId: string,
  jobId: string,
  callback: SseCallback,
): () => void {
  openForTeam(teamId);
  const existing = subscribers.get(jobId) ?? [];
  subscribers.set(jobId, [...existing, callback]);
  return () => {
    const cbs = subscribers.get(jobId) ?? [];
    const filtered = cbs.filter((cb) => cb !== callback);
    if (filtered.length === 0) subscribers.delete(jobId);
    else subscribers.set(jobId, filtered);
  };
}

export function subscribeSSEType(
  teamId: string,
  eventType: string,
  callback: SseCallback,
): () => void {
  openForTeam(teamId);
  const existing = typeSubscribers.get(eventType) ?? [];
  typeSubscribers.set(eventType, [...existing, callback]);
  return () => {
    const cbs = typeSubscribers.get(eventType) ?? [];
    const filtered = cbs.filter((cb) => cb !== callback);
    if (filtered.length === 0) typeSubscribers.delete(eventType);
    else typeSubscribers.set(eventType, filtered);
  };
}

export function closeSSE() {
  es?.close();
  es = null;
  currentTeamId = null;
  hadSuccessfulOpen = false;
  lastMessageAt = 0;
  clearDisconnectTimer();
  if (watchdogInterval !== null) {
    clearInterval(watchdogInterval);
    watchdogInterval = null;
  }
  subscribers.clear();
  typeSubscribers.clear();
  resetSSEConnection();
}

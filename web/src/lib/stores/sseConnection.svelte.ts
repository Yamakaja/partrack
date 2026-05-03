export type SSEConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'disconnected';

interface SSEConnectionStatus {
  state: SSEConnectionState;
  lastConnected: Date | null;
  reconnectCount: number;
}

export const sseConnectionStore = $state<{ current: SSEConnectionStatus }>({
  current: { state: 'connecting', lastConnected: null, reconnectCount: 0 },
});

export function markSSEConnected(): void {
  const prev = sseConnectionStore.current;
  sseConnectionStore.current = {
    state: 'connected',
    lastConnected: new Date(),
    reconnectCount: prev.reconnectCount,
  };
}

export function markSSEReconnecting(): void {
  const prev = sseConnectionStore.current;
  // Only transition to 'reconnecting' if we've ever been connected.
  // If we've never connected, stay as 'connecting'.
  sseConnectionStore.current = {
    ...prev,
    state: prev.lastConnected !== null ? 'reconnecting' : 'connecting',
  };
}

export function markSSEReconnected(): void {
  const prev = sseConnectionStore.current;
  sseConnectionStore.current = {
    state: 'connected',
    lastConnected: new Date(),
    reconnectCount: prev.reconnectCount + 1,
  };
}

export function markSSEDisconnected(): void {
  sseConnectionStore.current = {
    ...sseConnectionStore.current,
    state: 'disconnected',
  };
}

export function resetSSEConnection(): void {
  sseConnectionStore.current = { state: 'disconnected', lastConnected: null, reconnectCount: 0 };
}

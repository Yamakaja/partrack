export interface ScanSessionState {
  id: string;
  teamId: string;
  token: string;
  connected: boolean;   // was ever connected (phone_connected in DB)
  liveConnected: boolean; // SSE connection currently open (from scan_session_changed events)
}

export const scanSessionStore = $state<{ current: ScanSessionState | null }>({ current: null });

export function setScanSession(s: ScanSessionState): void {
  scanSessionStore.current = s;
}

export function markScanSessionConnected(): void {
  if (scanSessionStore.current) {
    scanSessionStore.current = { ...scanSessionStore.current, connected: true, liveConnected: true };
  }
}

export function markScanSessionDisconnected(): void {
  if (scanSessionStore.current) {
    scanSessionStore.current = { ...scanSessionStore.current, liveConnected: false };
  }
}

export function clearScanSession(): void {
  scanSessionStore.current = null;
}

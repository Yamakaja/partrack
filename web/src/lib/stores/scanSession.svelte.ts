export interface ScanSessionState {
  id: string;
  teamId: string;
  orderId: string;
  orderRef: string;
  token: string;
  connected: boolean;
}

export const scanSessionStore = $state<{ current: ScanSessionState | null }>({ current: null });

export function setScanSession(s: ScanSessionState): void {
  scanSessionStore.current = s;
}

export function markScanSessionConnected(): void {
  if (scanSessionStore.current) {
    scanSessionStore.current = { ...scanSessionStore.current, connected: true };
  }
}

export function clearScanSession(): void {
  scanSessionStore.current = null;
}

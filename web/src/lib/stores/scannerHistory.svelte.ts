export interface ScanHistoryEntry {
  partId: string;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  imageUrl: string | null;
  storageLocation: string | null;
  quantityOnHand: number | null;
  inventoryEntryId: string | null;
  timestamp: number;
}

const HISTORY_KEY = 'partrack-scan-history';
const MAX_ENTRIES = 100;

function load(): ScanHistoryEntry[] {
  try {
    return JSON.parse(localStorage.getItem(HISTORY_KEY) ?? '[]');
  } catch {
    return [];
  }
}

let scanHistory = $state<ScanHistoryEntry[]>(load());

export function recordVisit(entry: Omit<ScanHistoryEntry, 'timestamp'>) {
  const idx = scanHistory.findIndex(i => i.partId === entry.partId);
  if (idx !== -1) scanHistory.splice(idx, 1);
  scanHistory.unshift({ ...entry, timestamp: Date.now() });
  if (scanHistory.length > MAX_ENTRIES) scanHistory.splice(MAX_ENTRIES);
  localStorage.setItem(HISTORY_KEY, JSON.stringify(scanHistory));
}

export { scanHistory };

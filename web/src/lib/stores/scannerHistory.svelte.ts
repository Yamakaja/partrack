// Recently-viewed parts for the mobile scanner's "Recent" list.
//
// Privacy: history is namespaced per team and holds part IDENTITY only (no quantities,
// no storage locations). Stock/location are re-derived when a part is opened (PartDetail
// re-fetches). This prevents a shared/loaner phone re-paired to a different team from
// rendering the prior team's parts, and avoids persisting inventory levels on the device.

export interface ScanHistoryEntry {
  partId: string;
  mpn: string | null;
  manufacturer: string | null;
  description: string | null;
  imageUrl: string | null;
  timestamp: number;
}

const KEY_PREFIX = 'partrack-scan-history:';
const LEGACY_KEY = 'partrack-scan-history'; // pre-namespacing global key — purged on use
const MAX_ENTRIES = 100;

let currentTeamId: string | null = null;
let scanHistory = $state<ScanHistoryEntry[]>([]);

function keyFor(teamId: string): string {
  return KEY_PREFIX + teamId;
}

function load(teamId: string): ScanHistoryEntry[] {
  try {
    return JSON.parse(localStorage.getItem(keyFor(teamId)) ?? '[]');
  } catch {
    return [];
  }
}

// setHistoryTeam activates the given team's history, swapping the in-memory list in
// place so one team's parts never render under another (the core cross-tenant fix).
// Call it once the paired session's team is known. Passing null clears the view.
export function setHistoryTeam(teamId: string | null): void {
  localStorage.removeItem(LEGACY_KEY); // drop any unscoped history from older builds
  currentTeamId = teamId;
  scanHistory.splice(0, scanHistory.length, ...(teamId ? load(teamId) : []));
}

// clearHistory resets the in-memory view (e.g. on session expiry). The per-team
// persisted copy is kept, so re-pairing the SAME team restores its list; pairing a
// different team loads that team's list via setHistoryTeam instead.
export function clearHistory(): void {
  currentTeamId = null;
  scanHistory.splice(0, scanHistory.length);
}

export function recordVisit(entry: Omit<ScanHistoryEntry, 'timestamp'>): void {
  if (!currentTeamId) return; // no team context yet — don't record
  const idx = scanHistory.findIndex((i) => i.partId === entry.partId);
  if (idx !== -1) scanHistory.splice(idx, 1);
  scanHistory.unshift({ ...entry, timestamp: Date.now() });
  if (scanHistory.length > MAX_ENTRIES) scanHistory.splice(MAX_ENTRIES);
  localStorage.setItem(keyFor(currentTeamId), JSON.stringify(scanHistory));
}

export { scanHistory };

import { request } from './client.js';

export interface KittingProgress {
  pending: number;
  confirmed: number;
  shortfall: number;
  skipped: number;
}

export interface KittingSessionInfo {
  session: {
    id: string;
    build_id: string;
    build_ref: string;
    status: string;
  };
  progress: KittingProgress;
}

export interface KittingItem {
  part_id: string;
  part_mpn: string | null;
  part_manufacturer: string | null;
  part_description: string | null;
  quantity_needed: number;
  quantity_confirmed: number;
  status: 'pending' | 'confirmed' | 'shortfall' | 'skipped';
  storage_location_id: string | null;
  storage_location_path: string | null;
  storage_coordinate: number[] | null;
}

export function startKittingSession(
  teamId: string,
  sessionId: string,
  buildId: string,
): Promise<{ kitting_session_id: string }> {
  return request('POST', `/api/v1/teams/${teamId}/scan-sessions/${sessionId}/kitting`, {
    build_id: buildId,
  });
}

export function getKittingSession(teamId: string, kittingSessionId: string): Promise<KittingSessionInfo> {
  return request('GET', `/api/v1/teams/${teamId}/kitting/${kittingSessionId}`);
}

export function getKittingItems(teamId: string, kittingSessionId: string): Promise<{ items: KittingItem[] }> {
  return request('GET', `/api/v1/teams/${teamId}/kitting/${kittingSessionId}/items`);
}

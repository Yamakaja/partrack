import { request, qs } from './client.js';

export interface ScanAuditEntry {
  id: string;
  event_type: string;
  entity_type: string | null;
  entity_id: string | null;
  entity_name: string | null;
  quantity: number | null;
  parsed_sku: string | null;
  parsed_distributor_id: string | null;
  parsed_mpn: string | null;
  parsed_manufacturer: string | null;
  metadata: Record<string, unknown> | null;
  created_at: string;
}

export interface ScanAuditLogPage {
  items: ScanAuditEntry[];
  next_cursor_time: string | null;
  next_cursor_id: string | null;
}

export function listScanAuditLog(
  teamId: string,
  opts?: {
    eventTypes?: string[];
    cursorTime?: string;
    cursorId?: string;
    limit?: number;
  },
): Promise<ScanAuditLogPage> {
  const params: Record<string, string> = {};
  if (opts?.eventTypes?.length) params.event_type = opts.eventTypes.join(',');
  if (opts?.cursorTime) params.cursor_time = opts.cursorTime;
  if (opts?.cursorId) params.cursor_id = opts.cursorId;
  if (opts?.limit) params.limit = String(opts.limit);
  return request<ScanAuditLogPage>('GET', `/api/v1/teams/${teamId}/scan-audit-log?${qs(params)}`);
}

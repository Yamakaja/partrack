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
  part_manufacturer: string | null;
  part_short_description: string | null;
  part_image_url: string | null;
  part_category_name: string | null;
  inventory_entry_id: string | null;
  inventory_quantity_on_hand: number | null;
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

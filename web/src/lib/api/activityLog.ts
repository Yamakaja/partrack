import { request, qs } from './client.js';

export interface ActivitySubject {
  type: string;
  id: string;
  label: string;
}

export interface ActivityEntry {
  id: string;
  action: string;
  feed: 'team' | 'personal' | 'audit';
  source: 'web' | 'scanner' | 'job' | 'system';
  actor_label: string | null;
  subject: ActivitySubject | null;
  summary: string | null;
  payload: Record<string, unknown> | null;
  created_at: string;
  part_manufacturer: string | null;
  part_image_url: string | null;
  inventory_entry_id: string | null;
  inventory_quantity_on_hand: number | null;
}

export interface ActivityLogPage {
  items: ActivityEntry[];
  next_cursor_time: string | null;
  next_cursor_id: string | null;
}

export function listActivity(
  teamId: string,
  opts?: {
    scope?: 'mine' | 'team';
    actions?: string[];
    cursorTime?: string;
    cursorId?: string;
    limit?: number;
  },
): Promise<ActivityLogPage> {
  const params: Record<string, string> = {};
  if (opts?.scope === 'team') params.scope = 'team';
  if (opts?.actions?.length) params.action = opts.actions.join(',');
  if (opts?.cursorTime) params.cursor_time = opts.cursorTime;
  if (opts?.cursorId) params.cursor_id = opts.cursorId;
  if (opts?.limit) params.limit = String(opts.limit);
  return request<ActivityLogPage>('GET', `/api/v1/teams/${teamId}/activity-log?${qs(params)}`);
}

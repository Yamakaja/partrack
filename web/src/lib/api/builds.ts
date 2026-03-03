import { request, qs } from './client.js';
import type { Build, BuildConsumedItem } from '$lib/types/orders.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface BuildPreviewItem {
  bom_line_item_id: string;
  reference_designators: string;
  resolved_part_id: string | null;
  resolved_part_mpn: string | null;
  quantity_per_board: number;
  total_quantity: number;
  quantity_on_hand: number;
  sufficient: boolean;
  substitutes: { part_id: string; part_mpn: string | null; quantity_on_hand: number }[];
}

export function listBuilds(teamId: string, params: { limit?: number; offset?: number } = {}) {
  return request<PaginatedResponse<Build>>('GET', `/api/v1/teams/${teamId}/builds?${qs(params)}`);
}

export function previewBuild(teamId: string, body: { bom_revision_id: string; boards_built: number }) {
  return request<{ items: BuildPreviewItem[] }>('POST', `/api/v1/teams/${teamId}/builds/preview`, body);
}

export function createBuild(teamId: string, body: {
  project_id: string;
  bom_revision_id: string;
  boards_built: number;
  notes?: string;
  substitute_selections?: { bom_line_item_id: string; part_id: string }[];
  allow_stock_reconciliation?: boolean;
}) {
  return request<Build>('POST', `/api/v1/teams/${teamId}/builds`, body);
}

export function getBuild(teamId: string, buildId: string) {
  return request<Build & { items: BuildConsumedItem[] }>('GET', `/api/v1/teams/${teamId}/builds/${buildId}`);
}

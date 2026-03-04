import { request, qs } from './client.js';
import type { Build, BuildConsumedItem, BuildPartAllocation } from '$lib/types/orders.js';
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

export function listBuilds(
  teamId: string,
  params: { status?: string; project_id?: string; limit?: number; offset?: number } = {},
) {
  return request<PaginatedResponse<Build>>('GET', `/api/v1/teams/${teamId}/builds?${qs(params)}`);
}

export function getBuild(teamId: string, buildId: string) {
  return request<Build & {
    items: BuildConsumedItem[];
    allocations: BuildPartAllocation[];
    linked_order_ids: string[];
  }>('GET', `/api/v1/teams/${teamId}/builds/${buildId}`);
}

export function createBuild(
  teamId: string,
  body: { project_id: string; bom_revision_id: string; quantity: number; notes?: string },
) {
  return request<Build>('POST', `/api/v1/teams/${teamId}/builds`, body);
}

export function submitBuild(teamId: string, buildId: string) {
  return request<Build>('POST', `/api/v1/teams/${teamId}/builds/${buildId}/submit`);
}

export function checkoutBuild(
  teamId: string,
  buildId: string,
  body: {
    assembled_quantity?: number;
    substitute_selections?: { bom_line_item_id: string; part_id: string }[];
    allow_stock_reconciliation?: boolean;
  },
) {
  return request<{ id: string; status: string; deduction_count: number }>(
    'POST',
    `/api/v1/teams/${teamId}/builds/${buildId}/checkout`,
    body,
  );
}

export function updateBuild(
  teamId: string,
  buildId: string,
  body: { quantity?: number; notes?: string },
) {
  return request<Build>('PATCH', `/api/v1/teams/${teamId}/builds/${buildId}`, body);
}

export function cancelBuild(teamId: string, buildId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/builds/${buildId}`);
}

export function toggleBuildCartInclusion(teamId: string, buildId: string, excluded: boolean) {
  return request<void>(
    'PATCH',
    `/api/v1/teams/${teamId}/builds/${buildId}/cart-inclusion`,
    { excluded },
  );
}

export function previewBuild(
  teamId: string,
  body: { bom_revision_id: string; quantity: number },
) {
  return request<{ items: BuildPreviewItem[] }>('POST', `/api/v1/teams/${teamId}/builds/preview`, body);
}

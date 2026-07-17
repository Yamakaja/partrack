import { request, qs } from './client.js';
import type { Build, BuildConsumedItem, BuildPartAllocation } from '$lib/types/orders.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface BuildPreviewItem {
  bom_line_item_id: string;
  reference_designators: string;
  resolved_part_id: string | null;
  resolved_part_mpn: string | null;
  resolved_part_manufacturer: string | null;
  resolved_part_lifecycle_status: string | null;
  resolved_part_image_url: string | null;
  resolved_part_description: string | null;
  quantity_per_board: number;
  effective_quantity_per_board: number;
  total_quantity: number;
  effective_total_quantity: number;
  quantity_on_hand: number;
  sufficient: boolean;
  excluded_designator_count: number;
  override: 'use_available' | 'skip' | null;
  available_quantity: number | null;
  substitutes: { part_id: string; part_mpn: string | null; part_description: string | null; quantity_on_hand: number }[];
}

export function listBuilds(
  teamId: string,
  params: {
    status?: string | string[];
    project_id?: string;
    created_by?: string;
    sort_by?: string;
    sort_dir?: 'asc' | 'desc';
    limit?: number;
    offset?: number;
  } = {},
) {
  const { status, ...rest } = params;
  const statuses = status === undefined ? [] : Array.isArray(status) ? status : [status];
  const statusParams = statuses.map((s) => `status=${encodeURIComponent(s)}`).join('&');
  const base = qs(rest);
  const query = [base, statusParams].filter(Boolean).join('&');
  return request<PaginatedResponse<Build>>('GET', `/api/v1/teams/${teamId}/builds?${query}`);
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
  },
) {
  return request<{ id: string; status: string; deduction_count: number }>(
    'POST',
    `/api/v1/teams/${teamId}/builds/${buildId}/checkout`,
    body,
  );
}

export function getBuildPreview(teamId: string, buildId: string) {
  return request<{ items: BuildPreviewItem[] }>(
    'GET',
    `/api/v1/teams/${teamId}/builds/${buildId}/preview`,
  );
}

export function setExcludedDesignators(teamId: string, buildId: string, designators: string[]) {
  return request<void>(
    'PUT',
    `/api/v1/teams/${teamId}/builds/${buildId}/excluded-designators`,
    { designators },
  );
}

export function setLineItemOverride(
  teamId: string,
  buildId: string,
  itemId: string,
  overrideType: 'use_available' | 'skip' | null,
) {
  return request<void>(
    'PATCH',
    `/api/v1/teams/${teamId}/builds/${buildId}/line-item-overrides/${itemId}`,
    { override_type: overrideType },
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

export function getBuildPipeline(teamId: string) {
  return request<{
    draft: Build[];
    pending: Build[];
    ordered: Build[];
    ready: Build[];
  }>('GET', `/api/v1/teams/${teamId}/builds/pipeline`);
}

export function patchBuildSubstituteSelections(
  teamId: string,
  buildId: string,
  selections: { bom_line_item_id: string; part_id: string | null }[],
) {
  return request<{
    selections: { bom_line_item_id: string; selected_part_id: string; selected_part_mpn: string | null }[];
    warnings: { bom_line_item_id: string; part_id: string; message: string }[];
  }>('PATCH', `/api/v1/teams/${teamId}/builds/${buildId}/substitute-selections`, { selections });
}

export function refreshBuildSubstituteSelections(teamId: string, buildId: string) {
  return request<{ status: string }>('POST', `/api/v1/teams/${teamId}/builds/${buildId}/substitute-selections/refresh`);
}

import { request } from './client.js';
import type { Revision, RevisionDetail, BOMLineItem, StockCheckResult } from '$lib/types/projects.js';

export function listRevisions(teamId: string, projectId: string) {
  return request<{ items: Revision[] }>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions`);
}

export interface ImportRevisionItem {
  reference_designators: string;
  quantity: number;
  raw_description?: string;
  manufacturer?: string;
  mpn?: string;
  name?: string;
  sku?: string;
  distributor_name?: string;
  resolved_distributor_id?: string | null;
}

export interface BOMImportConfig {
  delimiter?: string;
  quoteChar?: string;
  escapeChar?: string;
  hasHeader?: string;
  comments?: string;
  skipFirstLines?: string;
  ref_col?: string;
  qty_col?: string;
  mpn_col?: string;
  description_col?: string;
  manufacturer_col?: string;
  name_col?: string;
  sku_col?: string;
  distributor_col?: string;
  /** distributor name → distributor UUID mapping chosen at import time */
  mapping?: Record<string, string | undefined>;
}

export interface ImportRevisionBody {
  label: string;
  notes?: string;
  set_active?: boolean;
  bom_import_config?: BOMImportConfig | null;
  items?: ImportRevisionItem[];
}

export function importRevision(teamId: string, projectId: string, body: ImportRevisionBody) {
  return request<{ job_id: string; bom_revision_id: string }>(
    'POST', `/api/v1/teams/${teamId}/projects/${projectId}/revisions`, body,
  );
}

export function getRevision(teamId: string, projectId: string, revId: string) {
  return request<RevisionDetail>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}`);
}

export function updateRevision(teamId: string, projectId: string, revId: string, body: { label?: string; notes?: string }) {
  return request<Revision>('PATCH', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}`, body);
}

export function updateBOMLineItem(teamId: string, projectId: string, revId: string, itemId: string, body: { resolved_part_id?: string | null; ignored?: boolean }) {
  return request<BOMLineItem>('PATCH', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}`, body);
}

export function enrichBOMLineItem(
  teamId: string,
  projectId: string,
  revId: string,
  itemId: string,
  body: { sku: string; distributor_id: string },
) {
  return request<{ part_id: string; part_mpn: string; part_manufacturer: string }>(
    'POST',
    `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/enrich`,
    body,
  );
}

export interface SubstituteItem {
  bom_line_item_id: string;
  part_id: string;
  part_mpn: string | null;
  priority: number;
}

export interface LineItemSubstituteItem {
  part_id: string;
  part_mpn: string | null;
  priority: number;
  manufacturer: string | null;
  description: string | null;
  lifecycle_status: string;
  quantity_on_hand: number | null;
}

export interface PreviousRevisionItem {
  id: string;
  label: string;
  imported_at: string;
}

export function listAllSubstitutes(teamId: string, projectId: string, revId: string) {
  return request<{ items: SubstituteItem[] }>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/substitutes`);
}

export function listLineItemSubstitutes(teamId: string, projectId: string, revId: string, itemId: string) {
  return request<{ items: LineItemSubstituteItem[] }>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes`);
}

export function addSubstitute(teamId: string, projectId: string, revId: string, itemId: string, partId: string) {
  return request<{ part_id: string; priority: number }>('POST', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes`, { part_id: partId });
}

export function removeSubstitute(teamId: string, projectId: string, revId: string, itemId: string, partId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes/${partId}`);
}

export function reorderSubstitutes(teamId: string, projectId: string, revId: string, itemId: string, partIds: string[]) {
  return request<void>('PUT', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes/order`, { part_ids: partIds });
}

export function importSubstitutes(teamId: string, projectId: string, revId: string, itemId: string, sourceRevisionId: string) {
  return request<void>('POST', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes/import`, { source_revision_id: sourceRevisionId });
}

export function listPreviousRevisionsWithSubstitutes(teamId: string, projectId: string, revId: string, itemId: string) {
  return request<{ items: PreviousRevisionItem[] }>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes/previous-revisions`);
}

export function getStockCheck(teamId: string, projectId: string, revId: string, buildQty: number) {
  return request<StockCheckResult>(
    'GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/stock-check?build_quantity=${buildQty}`,
  );
}

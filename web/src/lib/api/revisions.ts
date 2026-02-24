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

export interface ImportRevisionBody {
  label: string;
  notes?: string;
  set_active?: boolean;
  bom_import_config?: Record<string, string> | null;
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

export function updateBOMLineItem(teamId: string, projectId: string, revId: string, itemId: string, body: { resolved_part_id?: string | null }) {
  return request<BOMLineItem>('PATCH', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}`, body);
}

export interface SubstituteItem {
  bom_line_item_id: string;
  part_id: string;
  part_mpn: string;
}

export function listAllSubstitutes(teamId: string, projectId: string, revId: string) {
  return request<{ items: SubstituteItem[] }>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/substitutes`);
}

export function addSubstitute(teamId: string, projectId: string, revId: string, itemId: string, partId: string) {
  return request<void>('POST', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes`, { part_id: partId });
}

export function removeSubstitute(teamId: string, projectId: string, revId: string, itemId: string, partId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/substitutes/${partId}`);
}

export function getStockCheck(teamId: string, projectId: string, revId: string, buildQty: number) {
  return request<StockCheckResult>(
    'GET', `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/stock-check?build_quantity=${buildQty}`,
  );
}

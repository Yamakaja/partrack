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
  value?: string;
  footprint?: string;
  /** Reviewer-confirmed interpretation override (persisted as source 'user'). */
  interpretation?: Interpretation;
  interpretation_source?: string;
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
  value_col?: string;
  footprint_col?: string;
  /** distributor name → distributor UUID mapping chosen at import time */
  mapping?: Record<string, string | undefined>;
  /** distributor UUID applied to all SKUs when the BOM has no distributor column */
  single_distributor_id?: string;
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

export type InterpretRoute = 'identity' | 'parametric' | 'ignore' | 'unknown';

export interface InterpretParam {
  slug: string;
  raw: string;
  value_si: number | null;
}

/** Slug under which a part's package/case is stored as a parameter. */
export const PACKAGE_SLUG = 'package';

/** Raw value of a param by slug (e.g. the package), or undefined. */
export function paramRaw(
  params: InterpretParam[] | null | undefined,
  slug: string,
): string | undefined {
  return (params ?? []).find((p) => p.slug === slug)?.raw || undefined;
}

/** How the LLM handled a row — surfaced behind the AI indicator (review-only). */
export interface AiProvenance {
  /** Model that produced the reading. */
  model?: string;
  /** One-line summary of what the AI decided. */
  summary?: string;
  /** Reasoning trace, when the provider returns one. */
  reasoning?: string;
  /** Reasoning token count, when the provider reports it. */
  reasoning_tokens?: number;
}

/** Structured reading of one BOM row produced by the genspec parser. */
export interface Interpretation {
  route: InterpretRoute;
  class: string;
  category: string;
  mounting: string;
  /** Extracted constraints, including the "package" param. */
  params: InterpretParam[];
  flags: string[];
  confidence: number;
  /** Provenance: 'auto' (fresh parse), 'reused' (team memory), or 'llm' (LLM fallback). */
  source?: 'auto' | 'reused' | 'llm';
  /** True when this came from the team's remembered corrections, not a fresh parse. */
  reused?: boolean;
  /** Ephemeral record of how the LLM read this row (present only when source === 'llm'). */
  ai?: AiProvenance;
  /** Identity discovered by the LLM specific-part fallback (overlaid onto line columns at import). */
  mpn?: string;
  manufacturer?: string;
  sku?: string;
  distributor_id?: string;
}

export interface InterpretInput {
  refdes?: string;
  footprint?: string;
  value?: string;
  mpn?: string;
  sku?: string;
  /** Original BOM header → cell value, given to the LLM fallback for context. */
  row?: Record<string, string>;
}

/**
 * Parse mapped BOM rows into interpretations (no persistence). When teamId is
 * given, the team's remembered corrections are layered over the fresh parse and
 * the affected rows are flagged `reused` — matching what the import will persist.
 *
 * With `stream: true`, the LLM fallback runs asynchronously: the response returns
 * the immediate (deterministic/memory) readings plus a `job_id` and the `pending`
 * row indices being read by the AI; each result then streams over SSE
 * (`bom_interpret_item`, then `bom_interpret_complete`). Without it, LLM rows are
 * resolved inline before the response (used for single-row re-parse).
 */
export function interpretPreview(items: InterpretInput[], teamId?: string, opts?: { stream?: boolean }) {
  return request<{ items: Interpretation[]; job_id?: string; pending?: number[] }>(
    'POST',
    '/api/v1/bom/interpret-preview',
    {
      items,
      ...(teamId ? { team_id: teamId } : {}),
      ...(opts?.stream ? { stream: true } : {}),
    },
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

/** A catalog part matching a generic line's parametric spec (review candidate picker). */
export interface GenericCandidate {
  part_id: string;
  mpn: string | null;
  manufacturer: string | null;
  package: string | null;
  description: string | null;
  value_si: number | null;
  scope: 'global' | 'team';
}

/** Existing catalog parts that satisfy a generic line's interpretation (category +
    package + value within tolerance) — the same set the importer's match-only tier
    considered. Empty for non-parametric lines or when nothing matches. */
export function listGenericCandidates(teamId: string, projectId: string, revId: string, itemId: string) {
  return request<{ items: GenericCandidate[] }>(
    'GET',
    `/api/v1/teams/${teamId}/projects/${projectId}/revisions/${revId}/items/${itemId}/generic-candidates`,
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

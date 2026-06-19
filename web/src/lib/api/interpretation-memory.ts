import { request } from './client.js';
import type { Interpretation } from './revisions.js';

/**
 * One remembered BOM-line interpretation for a project (doc/bom-import.md §9.2).
 * Keyed by the normalised (class, value, footprint); the import review gate writes
 * these, and every later import of the same project reuses them.
 */
export interface InterpretationMemoryEntry {
  id: string;
  class: string;
  value_key: string;
  footprint_key: string;
  interpretation: Interpretation;
  created_by: string | null;
  created_by_name: string | null;
  created_at: string;
  updated_at: string;
}

export function listInterpretationMemory(teamId: string, projectId: string) {
  return request<{ items: InterpretationMemoryEntry[] }>(
    'GET',
    `/api/v1/teams/${teamId}/projects/${projectId}/interpretation-memory`,
  );
}

export function deleteInterpretationMemoryEntry(teamId: string, projectId: string, memoryId: string) {
  return request<void>(
    'DELETE',
    `/api/v1/teams/${teamId}/projects/${projectId}/interpretation-memory/${memoryId}`,
  );
}

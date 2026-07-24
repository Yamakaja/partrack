import { request, qs } from './client.js';
import type { ObjectKind, ObjectSummary } from '$lib/types/objectRef.js';

/** Server-side cap on one batch; the resolver chunks to stay under it. */
export const MAX_OBJECT_REFS_PER_REQUEST = 200;

/**
 * Resolve ids the caller already holds but has no display fields for.
 *
 * Prefer the resolver in $lib/objectref/resolver.svelte.ts over calling this
 * directly — it coalesces every unresolved id on screen into one request. Ids
 * the team cannot see are simply absent from the result.
 */
export function resolveObjectRefs(
  teamId: string,
  byKind: Partial<Record<ObjectKind, string[]>>,
): Promise<Record<string, ObjectSummary>> {
  const params: Record<string, string> = {};
  for (const [kind, ids] of Object.entries(byKind)) {
    if (ids && ids.length) params[kind] = ids.join(',');
  }
  return request<{ objects: Record<string, ObjectSummary> }>(
    'GET',
    `/api/v1/teams/${teamId}/object-refs?${qs(params)}`,
  ).then((r) => r.objects ?? {});
}

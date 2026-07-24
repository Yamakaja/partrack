import { resolveObjectRefs, MAX_OBJECT_REFS_PER_REQUEST } from '$lib/api/objectRefs.js';
import type { ObjectKind, ObjectSummary } from '$lib/types/objectRef.js';

/**
 * Shared resolution of object references, batched.
 *
 * A page can hold hundreds of refs, so each one owning a query would be an N+1
 * on render. Instead a ref *declares* the id it needs; every id declared during
 * a tick is collected and resolved in one request, and every ref reads the
 * result out of the same reactive store.
 *
 * Most refs never reach the network at all. A row that already carries its own
 * display fields — which is true of nearly every list query in the app — should
 * `seed()` them, and the id is then already resolved when the ref asks.
 *
 * Usage from a component:
 *
 *   $effect(() => { ensure(teamId, 'order', orderId); });
 *   const summary = $derived(lookup(teamId, orderId));
 *
 * `ensure` mutates, so it belongs in an effect; `lookup` is a pure read.
 */

// `undefined` = not asked yet or still in flight; `null` = asked and
// unresolvable (deleted, or not visible to this team). The distinction lets a
// ref show its fallback label without flickering through a loading state.
const cache = $state<Record<string, ObjectSummary | null>>({});

const pending = new Map<string, Map<ObjectKind, Set<string>>>();
const inflight = new Set<string>();
let flushScheduled = false;

function cacheKey(teamId: string, id: string): string {
  return `${teamId}:${id}`;
}

/** Read a resolved summary. Undefined while unknown, null when unresolvable. */
export function lookup(teamId: string, id: string): ObjectSummary | null | undefined {
  return cache[cacheKey(teamId, id)];
}

/**
 * Declare that this id needs resolving. Idempotent and cheap: an id already
 * cached or already in flight is ignored, so calling it every render is fine.
 */
export function ensure(teamId: string, kind: ObjectKind, id: string): void {
  if (!teamId || !id) return;
  const key = cacheKey(teamId, id);
  if (key in cache || inflight.has(key)) return;

  inflight.add(key);
  let byKind = pending.get(teamId);
  if (!byKind) {
    byKind = new Map();
    pending.set(teamId, byKind);
  }
  let ids = byKind.get(kind);
  if (!ids) {
    ids = new Set();
    byKind.set(kind, ids);
  }
  ids.add(id);

  if (!flushScheduled) {
    flushScheduled = true;
    // A microtask fires after the whole effect batch has run, so every ref that
    // mounted in this render lands in the same request.
    queueMicrotask(flush);
  }
}

/**
 * Prime the cache from data a caller already has, so no request is made.
 * This is the fast path: list rows carry their own display fields.
 */
export function seed(teamId: string, summary: ObjectSummary): void {
  if (!teamId || !summary?.id) return;
  cache[cacheKey(teamId, summary.id)] = summary;
}

export function seedMany(teamId: string, summaries: ObjectSummary[]): void {
  for (const s of summaries) seed(teamId, s);
}

/**
 * Drop cached summaries so they resolve again — labels are snapshots of a
 * mutable thing (an order gains a PO number, a project is renamed). Call with
 * an id to drop one, or bare to drop everything.
 */
export function invalidate(teamId?: string, id?: string): void {
  if (teamId && id) {
    delete cache[cacheKey(teamId, id)];
    return;
  }
  const prefix = teamId ? `${teamId}:` : '';
  for (const key of Object.keys(cache)) {
    if (!prefix || key.startsWith(prefix)) delete cache[key];
  }
}

async function flush(): Promise<void> {
  flushScheduled = false;
  const batches = [...pending.entries()];
  pending.clear();

  await Promise.all(batches.map(([teamId, byKind]) => flushTeam(teamId, byKind)));
}

async function flushTeam(teamId: string, byKind: Map<ObjectKind, Set<string>>): Promise<void> {
  // Flatten so chunking respects the server's cap across kinds, not per kind.
  const pairs: Array<[ObjectKind, string]> = [];
  for (const [kind, ids] of byKind) {
    for (const id of ids) pairs.push([kind, id]);
  }

  for (let i = 0; i < pairs.length; i += MAX_OBJECT_REFS_PER_REQUEST) {
    const chunk = pairs.slice(i, i + MAX_OBJECT_REFS_PER_REQUEST);
    const params: Partial<Record<ObjectKind, string[]>> = {};
    for (const [kind, id] of chunk) (params[kind] ??= []).push(id);

    let resolved: Record<string, ObjectSummary> = {};
    try {
      resolved = await resolveObjectRefs(teamId, params);
    } catch {
      // Leave every id in this chunk marked unresolvable below rather than
      // retrying — a failed batch must not spin.
    }

    for (const [, id] of chunk) {
      const key = cacheKey(teamId, id);
      cache[key] = resolved[id] ?? null;
      inflight.delete(key);
    }
  }
}

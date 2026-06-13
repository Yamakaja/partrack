import { subscribeSSE } from '$lib/api/sse.js';
import { pushProgressToast, updateToast, settleToast, type ToastType } from '$lib/stores/toasts.svelte.js';

export interface BatchJobOptions {
  teamId: string;
  jobId: string;
  /** Total items the job will process; drives the progress bar. */
  total: number;
  /** Label shown while running. */
  label: string;
  /** SSE event type that carries the terminal summary (e.g. 'listing_refresh_complete'). */
  completeType: string;
  /** Build the settled toast from the terminal event's data payload. */
  result: (data: Record<string, unknown>) => { type: ToastType; message: string; detail?: string };
  /**
   * Called once when the job finishes, regardless of whether the user dismissed
   * the progress toast. Use to refresh affected views (the acting user is skipped
   * for the entity-change SSE events these jobs emit).
   */
  onDone?: (data: Record<string, unknown>) => void;
}

/**
 * Drives a persistent progress toast for a batch worker job. Subscribes to the
 * job's SSE events, advances the toast on each per-item event, and on the terminal
 * `completeType` event settles the toast into a normal auto-dismissing toast and
 * calls onDone.
 *
 * The SSE subscription is owned by the tracker, not the toast: if the user
 * dismisses the progress toast early the job still completes and onDone still
 * fires — the result is useful either way.
 */
export function trackBatchJob({
  teamId,
  jobId,
  total,
  label,
  completeType,
  result,
  onDone,
}: BatchJobOptions): () => void {
  const toastId = pushProgressToast(label, total);
  let current = 0;
  let finished = false;

  const finish = (data: Record<string, unknown>) => {
    if (finished) return;
    finished = true;
    unsub();
    const settled = result(data);
    settleToast(toastId, settled.type, settled.message, settled.detail);
    onDone?.(data);
  };

  const unsub = subscribeSSE(teamId, jobId, (ev) => {
    if (ev.type === completeType) {
      updateToast(toastId, { progress: { current: total, total } });
      finish(ev.data);
      return;
    }
    // Any other event delivered for this job id is a per-item progress event.
    current = Math.min(total, current + 1);
    updateToast(toastId, { progress: { current, total } });
  });

  return unsub;
}

/** Result formatter for batch listing (stock/price) refreshes. */
export function listingRefreshResult(d: Record<string, unknown>) {
  const done = (d.done as number) ?? 0;
  const unavailable = (d.unavailable as number) ?? 0;
  const failed = (d.failed as number) ?? 0;
  if (failed > 0 || unavailable > 0) {
    const parts: string[] = [];
    if (done) parts.push(`${done} updated`);
    if (unavailable) parts.push(`${unavailable} unavailable`);
    if (failed) parts.push(`${failed} failed`);
    return { type: (failed > 0 ? 'warning' : 'info') as ToastType, message: 'Stock refresh complete', detail: parts.join(' · ') };
  }
  return { type: 'success' as ToastType, message: 'Stock refreshed', detail: `${done} listing${done !== 1 ? 's' : ''} updated` };
}

/** Result formatter for batch parameter (enrichment) refreshes. */
export function partEnrichResult(d: Record<string, unknown>) {
  const done = (d.done as number) ?? 0;
  const notFound = (d.not_found as number) ?? 0;
  const failed = (d.failed as number) ?? 0;
  if (failed > 0 || notFound > 0) {
    const parts: string[] = [];
    if (done) parts.push(`${done} enriched`);
    if (notFound) parts.push(`${notFound} not found`);
    if (failed) parts.push(`${failed} failed`);
    return { type: (failed > 0 ? 'warning' : 'info') as ToastType, message: 'Parameter refresh complete', detail: parts.join(' · ') };
  }
  return { type: 'success' as ToastType, message: 'Parameters refreshed', detail: `${done} part${done !== 1 ? 's' : ''} enriched` };
}

/** Thin wrapper: track a batch listing (stock/price) refresh job. */
export function trackListingRefresh(p: { teamId: string; jobId: string; total: number; onDone?: () => void }) {
  return trackBatchJob({
    ...p,
    label: 'Refreshing stock & prices…',
    completeType: 'listing_refresh_complete',
    result: listingRefreshResult,
  });
}

/** Thin wrapper: track a batch parameter (enrichment) refresh job. */
export function trackPartEnrich(p: { teamId: string; jobId: string; total: number; onDone?: () => void }) {
  return trackBatchJob({
    ...p,
    label: 'Refreshing parameters…',
    completeType: 'part_enrich_complete',
    result: partEnrichResult,
  });
}

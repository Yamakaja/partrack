<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Sparkles } from 'lucide-svelte';
  import { interpretPreview, type Interpretation, type InterpretRoute } from '$lib/api/revisions.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import type { BOMImportItem } from './bom-import-utils.js';
  import InterpretationEditor from './InterpretationEditor.svelte';
  import AiIndicator from '$lib/components/ai/AiIndicator.svelte';

  let {
    items = $bindable(),
    interpretations = $bindable(),
    edited = $bindable(),
    teamId,
    loading,
    jobId = '',
    pending = [],
    onback,
    onnext,
    oncancel,
  }: {
    items: BOMImportItem[];
    interpretations: Interpretation[];
    edited: Record<number, boolean>;
    teamId: string;
    loading: boolean;
    /** Correlation id for the async LLM read; empty when nothing is streaming. */
    jobId?: string;
    /** Row indices still being read by the AI when the step opened. */
    pending?: number[];
    onback: () => void;
    onnext: () => void;
    oncancel: () => void;
  } = $props();

  // Live AI-read state: rows still pending and rows whose AI read failed.
  let pendingSet = $state(new Set<number>());
  let failedSet = $state(new Set<number>());

  // Subscribe to the per-row LLM stream once a job id is available. The first event
  // is gated behind an LLM round-trip, so subscribing here (after the preview
  // response) never misses one. Cleans up on unmount / step change.
  $effect(() => {
    if (!jobId) {
      pendingSet = new Set();
      failedSet = new Set();
      return;
    }
    pendingSet = new Set(pending);
    failedSet = new Set();
    const unsub = subscribeSSE(teamId, jobId, (ev) => {
      if (ev.type === 'bom_interpret_item') {
        const idx = ev.data.index as number;
        if (ev.data.status === 'ok' && ev.data.item) {
          interpretations[idx] = ev.data.item as Interpretation;
        } else {
          failedSet.add(idx);
          failedSet = new Set(failedSet);
        }
        pendingSet.delete(idx);
        pendingSet = new Set(pendingSet);
      } else if (ev.type === 'bom_interpret_complete') {
        // Any leftover pending row (a dropped event) keeps its deterministic reading.
        pendingSet = new Set();
      }
    });
    return unsub;
  });

  const ROUTE_BADGE: Record<InterpretRoute, { label: string; variant: 'success' | 'default' | 'warning' | 'secondary' }> = {
    parametric: { label: 'Generic', variant: 'default' },
    identity: { label: 'Specific', variant: 'success' },
    ignore: { label: 'Ignored', variant: 'secondary' },
    unknown: { label: 'Review', variant: 'warning' },
  };

  function needsAttention(it: Interpretation): boolean {
    if (it.source === 'llm') return true; // AI read a weird row — worth a glance
    if (it.reused) return false; // a remembered correction was already confirmed
    return it.route === 'unknown' || (it.flags?.length ?? 0) > 0 || (it.confidence ?? 1) < 0.7;
  }

  // Attention score: higher sorts to the top. AI-read and problematic/low-confidence
  // rows first; confident rows below for a quick scroll-through (they can still be
  // wrong). Remembered corrections sink to the bottom — they need the least attention.
  function score(it: Interpretation): number {
    if (it.source === 'llm') return 60;
    if (it.reused) return -1;
    let s = 0;
    if (it.route === 'unknown') s += 100;
    s += (it.flags?.length ?? 0) * 50;
    s += Math.round((1 - (it.confidence ?? 1)) * 40);
    return s;
  }

  // Rows still being read by the AI sort near the top so they stay visible while
  // resolving; once the result streams in, the row's own score takes over.
  let order = $derived(
    interpretations
      .map((it, i) => ({ i, s: score(it) + (pendingSet.has(i) ? 70 : 0) }))
      .sort((a, b) => b.s - a.s || a.i - b.i)
      .map((o) => o.i),
  );

  let summary = $derived.by(() => {
    const c: Record<InterpretRoute, number> = { parametric: 0, identity: 0, ignore: 0, unknown: 0 };
    for (const it of interpretations) c[it.route]++;
    return c;
  });

  let attentionCount = $derived(interpretations.filter(needsAttention).length);
  let reusedCount = $derived(interpretations.filter((it) => it.reused).length);
  let llmCount = $derived(interpretations.filter((it) => it.source === 'llm').length);
  let pendingCount = $derived(pendingSet.size);

  // Re-run the parser for one row after its source Value/Footprint is edited, so the
  // re-extracted parameters (and SI values) stay correct rather than going stale.
  // Single-row, so resolved synchronously (no streaming).
  async function reparse(i: number, value: string, footprint: string) {
    items[i].value = value;
    items[i].footprint = footprint;
    const res = await interpretPreview(
      [{ refdes: items[i].reference_designators, footprint, value, mpn: items[i].mpn, sku: items[i].sku }],
      teamId,
    );
    if (res.items[0]) {
      interpretations[i] = res.items[0];
      edited[i] = true;
      pendingSet.delete(i);
      pendingSet = new Set(pendingSet);
      failedSet.delete(i);
      failedSet = new Set(failedSet);
    }
  }
</script>

<div class="min-h-0 flex-1 overflow-auto px-6 py-4 space-y-3">
  {#if loading}
    <div class="py-12 text-center text-sm text-muted-foreground">Interpreting BOM rows…</div>
  {:else}
    <!-- Summary -->
    <div class="flex flex-wrap items-center gap-2 text-xs">
      <span class="text-muted-foreground">How each row was read:</span>
      <Badge variant="default">{summary.parametric} generic</Badge>
      <Badge variant="success">{summary.identity} specific</Badge>
      <Badge variant="secondary">{summary.ignore} ignored</Badge>
      {#if summary.unknown > 0}<Badge variant="warning">{summary.unknown} needs review</Badge>{/if}
      {#if llmCount > 0}<span title="Read by AI because the format was unusual — please verify"><Badge variant="warning">{llmCount} AI-read</Badge></span>{/if}
      {#if reusedCount > 0}<span title="Reused from your team's earlier corrections"><Badge variant="outline">{reusedCount} remembered</Badge></span>{/if}
      {#if pendingCount > 0}
        <span class="inline-flex items-center gap-1 text-primary">
          <AiIndicator loading />
          {pendingCount} row{pendingCount === 1 ? '' : 's'} still reading…
        </span>
      {/if}
      <span class="ml-auto text-muted-foreground">
        {#if attentionCount > 0}
          {attentionCount} row{attentionCount === 1 ? '' : 's'} need attention (sorted to top)
        {:else}
          Looks clean — scroll to spot-check
        {/if}
      </span>
    </div>

    <!-- Dense, attention-first list -->
    <div class="divide-y divide-border rounded-md border border-border">
      {#each order as i (i)}
        {@const it = interpretations[i]}
        {@const isPending = pendingSet.has(i)}
        <div class="relative flex flex-col gap-2 px-3 py-2 text-sm {isPending ? 'bg-muted/30' : needsAttention(it) ? 'bg-warning/5' : ''}">
          <!-- While the AI is still reading the row, dim + lock the controls and
               overlay a spinner so it's clearly in progress (not failed). -->
          <div class="flex flex-col gap-2 {isPending ? 'pointer-events-none select-none opacity-40' : ''}">
            <div class="flex items-center gap-2">
              <span class="font-medium">{items[i].reference_designators}</span>
              <Badge variant={ROUTE_BADGE[it.route].variant}>{ROUTE_BADGE[it.route].label}</Badge>
              {#if !isPending}
                {#if it.source === 'llm'}
                  <AiIndicator ai={it.ai} />
                {:else if failedSet.has(i)}
                  <AiIndicator failed />
                {/if}
              {/if}
              {#if it.reused}<span title="Reused from your team's earlier corrections"><Badge variant="outline">remembered</Badge></span>{/if}
              {#if edited[i]}<span class="text-[10px] uppercase tracking-wide text-primary">edited</span>{/if}
              {#each it.flags ?? [] as flag}
                <span class="text-[10px] rounded bg-warning/20 px-1.5 py-0.5 text-warning-foreground">{flag}</span>
              {/each}
            </div>

            <InterpretationEditor
              bind:interpretation={interpretations[i]}
              source={{
                reference_designators: items[i].reference_designators,
                value: items[i].value,
                footprint: items[i].footprint,
                mpn: items[i].mpn,
                sku: items[i].sku,
              }}
              {teamId}
              onchange={() => (edited[i] = true)}
              onReparse={(v, f) => reparse(i, v, f)}
            />
          </div>

          {#if isPending}
            <div class="absolute inset-0 z-10 flex items-center justify-center gap-2 bg-background/40">
              <div class="h-4 w-4 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
              <span class="inline-flex items-center gap-1 text-xs font-medium text-primary">
                <Sparkles class="h-3.5 w-3.5" /> Reading with AI…
              </span>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- Footer -->
<div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
  <Button variant="outline" onclick={onback}>← Back</Button>
  <div class="flex gap-2">
    <Button variant="outline" onclick={oncancel}>Cancel</Button>
    <Button onclick={onnext} disabled={loading}>Continue →</Button>
  </div>
</div>

<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity';
  import ScannerCamera from './ScannerCamera.svelte';
  import {
    listScanCollections, listScanBuilds, listOrders,
    checkTarget, checkScan, checkFinish,
    ScanExpiredError,
    type CheckSource, type CheckTargetEntry, type CheckScanResponse,
    type ScanCollection, type ScanBuildPick,
  } from '$lib/api/scanner.js';
  import type { ScanOrder } from './types.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { scanFeedback, scanPulse } from './feedback.svelte.js';

  // Filter mode: scan a pile of bags AGAINST a target set — big per-bag
  // verdicts, no writes anywhere. The phone owns the tally; the real product
  // of a session is the "never scanned" summary.
  let {
    initialCollectionId = null,
    initialCollectionName = null,
    onBack,
    onExpired,
  }: {
    /** Pre-selected list source (desktop "Filter on phone" push). */
    initialCollectionId?: string | null;
    initialCollectionName?: string | null;
    onBack: () => void;
    onExpired: () => void;
  } = $props();

  type Screen = 'source' | 'pick_list' | 'pick_build' | 'pick_order' | 'scanning' | 'summary';
  let screen = $state<Screen>('source');

  let source = $state<CheckSource | null>(null);
  let targetRef = $state('');
  let entries = $state<CheckTargetEntry[]>([]);
  let loading = $state(false);

  // Coverage: primary target parts seen (a substitute scan credits its
  // primary). Misses and unknowns never advance coverage.
  const seen = new SvelteSet<string>();
  let missCount = $state(0);
  let unknownCount = $state(0);
  const primaries = $derived(entries.filter((e) => !e.substitute));
  const total = $derived(primaries.length);

  // Verdict card, persists until the next scan.
  type Card =
    | { kind: 'match'; entry: CheckTargetEntry }
    | { kind: 'substitute'; entry: CheckTargetEntry }
    | { kind: 'miss'; mpn: string | null; description: string | null }
    | { kind: 'unknown' };
  let card = $state<Card | null>(null);
  let confirmLeave = $state(false);

  // Pickers
  let lists = $state<ScanCollection[]>([]);
  let builds = $state<ScanBuildPick[]>([]);
  let orders = $state<ScanOrder[]>([]);
  const selectedOrders = new SvelteSet<string>();

  $effect(() => {
    if (initialCollectionId && screen === 'source' && !source) {
      void startWithSource(
        { type: 'collection', collection_id: initialCollectionId },
        initialCollectionName ?? 'list',
      );
    }
  });

  async function startWithSource(src: CheckSource, fallbackRef: string) {
    loading = true;
    try {
      const t = await checkTarget(src);
      if (t.entries.length === 0) {
        pushToast('warning', `Nothing to filter against — ${fallbackRef} is empty`);
        return;
      }
      source = src;
      targetRef = t.ref || fallbackRef;
      entries = t.entries;
      seen.clear();
      missCount = 0;
      unknownCount = 0;
      card = null;
      screen = 'scanning';
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not load the target set');
    } finally {
      loading = false;
    }
  }

  async function openListPicker() {
    screen = 'pick_list';
    try {
      lists = (await listScanCollections()).collections;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    }
  }

  async function openBuildPicker() {
    screen = 'pick_build';
    try {
      builds = (await listScanBuilds()).builds;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    }
  }

  async function openOrderPicker() {
    screen = 'pick_order';
    selectedOrders.clear();
    try {
      orders = (await listOrders()).orders;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    }
  }

  // ── Scan loop ─────────────────────────────────────────────────────────────

  let busy = $state(false);
  const seenBarcodes = new Map<string, number>();

  async function handleScan(raw: string) {
    if (!source || busy) return;
    const now = Date.now();
    const last = seenBarcodes.get(raw);
    seenBarcodes.set(raw, now);
    // Same bag again within the debounce window: no new verdict, but say so.
    if (last !== undefined && now - last <= 2500) { scanFeedback('notice'); return; }

    busy = true;
    try {
      const res: CheckScanResponse = await checkScan(raw, source, { done: seen.size, total });
      applyVerdict(res);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      scanFeedback('error');
      pushToast('error', 'Scan failed — try again');
    } finally {
      busy = false;
    }
  }

  function applyVerdict(res: CheckScanResponse) {
    if (res.verdict === 'match' && res.entry) {
      seen.add(res.entry.part_id);
      card = { kind: 'match', entry: res.entry };
      scanFeedback(seen.size === total ? 'complete' : 'success');
    } else if (res.verdict === 'substitute' && res.entry) {
      if (res.entry.substitute_for_part_id) seen.add(res.entry.substitute_for_part_id);
      card = { kind: 'substitute', entry: res.entry };
      scanFeedback('warning');
    } else if (res.verdict === 'miss') {
      missCount += 1;
      card = { kind: 'miss', mpn: res.part?.mpn ?? null, description: res.part?.description ?? null };
      scanFeedback('error');
    } else {
      unknownCount += 1;
      card = { kind: 'unknown' };
      scanFeedback('error');
    }
  }

  function requestLeave() {
    if (seen.size > 0 || missCount > 0 || unknownCount > 0) {
      confirmLeave = true;
    } else {
      onBack();
    }
  }

  function finish() {
    checkFinish(targetRef, seen.size, total).catch(() => {});
    screen = 'summary';
  }

  const neverScanned = $derived(primaries.filter((e) => !seen.has(e.part_id)));
</script>

<!-- ── SOURCE PICKER ───────────────────────────────────────────────────────── -->
{#if screen === 'source'}
  <div class="flex flex-1 flex-col overflow-hidden">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Filter against…</p>
    </div>
    <div class="flex-1 overflow-y-auto p-4 space-y-2">
      <button onclick={openListPicker} class="w-full rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800">
        <p class="font-medium">A list</p>
        <p class="text-xs text-zinc-400">Is this bag on one of my lists?</p>
      </button>
      <button onclick={openBuildPicker} class="w-full rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800">
        <p class="font-medium">A build</p>
        <p class="text-xs text-zinc-400">Does this bag belong to a build? Substitutes count.</p>
      </button>
      <button onclick={openOrderPicker} class="w-full rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800">
        <p class="font-medium">Orders</p>
        <p class="text-xs text-zinc-400">Was this bag part of a delivery?</p>
      </button>
    </div>
  </div>

<!-- ── LIST PICKER ─────────────────────────────────────────────────────────── -->
{:else if screen === 'pick_list'}
  <div class="flex flex-1 flex-col overflow-hidden">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => (screen = 'source')} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Filter by list</p>
    </div>
    <div class="flex-1 overflow-y-auto p-4 space-y-2">
      {#each lists as l (l.id)}
        <button
          disabled={loading}
          onclick={() => startWithSource({ type: 'collection', collection_id: l.id }, l.name)}
          class="w-full flex items-center justify-between gap-3 rounded-xl bg-zinc-900 px-4 py-3.5 text-left active:bg-zinc-800 disabled:opacity-50"
        >
          <div class="min-w-0">
            <p class="font-medium truncate">{l.name}</p>
            <p class="text-xs text-zinc-400">{l.item_count} part{l.item_count === 1 ? '' : 's'}</p>
          </div>
        </button>
      {:else}
        <p class="text-sm text-zinc-500 text-center py-6">No lists yet.</p>
      {/each}
    </div>
  </div>

<!-- ── BUILD PICKER ────────────────────────────────────────────────────────── -->
{:else if screen === 'pick_build'}
  <div class="flex flex-1 flex-col overflow-hidden">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => (screen = 'source')} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Filter by build</p>
    </div>
    <div class="flex-1 overflow-y-auto p-4 space-y-2">
      {#each builds as b (b.id)}
        <button
          disabled={loading}
          onclick={() => startWithSource({ type: 'build', build_id: b.id }, b.ref)}
          class="w-full flex items-center justify-between gap-3 rounded-xl bg-zinc-900 px-4 py-3.5 text-left active:bg-zinc-800 disabled:opacity-50"
        >
          <div class="min-w-0">
            <p class="font-medium truncate">{b.ref}</p>
            <p class="text-xs text-zinc-400 capitalize">{b.status}</p>
          </div>
        </button>
      {:else}
        <p class="text-sm text-zinc-500 text-center py-6">No builds yet.</p>
      {/each}
    </div>
  </div>

<!-- ── ORDER PICKER (multi-select) ─────────────────────────────────────────── -->
{:else if screen === 'pick_order'}
  <div class="flex flex-1 flex-col overflow-hidden">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => (screen = 'source')} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Filter by orders</p>
    </div>
    <div class="flex-1 overflow-y-auto p-4 space-y-2">
      {#each orders as o (o.id)}
        <button
          onclick={() => (selectedOrders.has(o.id) ? selectedOrders.delete(o.id) : selectedOrders.add(o.id))}
          class="w-full flex items-center gap-3 rounded-xl px-4 py-3.5 text-left active:bg-zinc-800
            {selectedOrders.has(o.id) ? 'bg-zinc-800 ring-1 ring-white/40' : 'bg-zinc-900'}"
        >
          <div class="flex h-5 w-5 shrink-0 items-center justify-center rounded border {selectedOrders.has(o.id) ? 'border-white bg-white text-black' : 'border-zinc-600'}">
            {#if selectedOrders.has(o.id)}
              <svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
              </svg>
            {/if}
          </div>
          <div class="min-w-0">
            <p class="font-medium truncate">{o.title}</p>
            <p class="text-xs text-zinc-400">{o.distributor_name} · {o.item_count} line{o.item_count === 1 ? '' : 's'}</p>
          </div>
        </button>
      {:else}
        <p class="text-sm text-zinc-500 text-center py-6">No receivable orders.</p>
      {/each}
    </div>
    <div class="p-4 border-t border-zinc-800">
      <button
        disabled={selectedOrders.size === 0 || loading}
        onclick={() => startWithSource({ type: 'order', order_ids: [...selectedOrders] }, 'orders')}
        class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200 disabled:opacity-40"
      >
        Start ({selectedOrders.size})
      </button>
    </div>
  </div>

<!-- ── SCANNING ────────────────────────────────────────────────────────────── -->
{:else if screen === 'scanning'}
  <div class="relative flex flex-1 flex-col overflow-hidden">
    <ScannerCamera onBack={requestLeave} onScan={handleScan} label={`Filter: ${targetRef}`}>
      {#snippet topRightControls()}
        <button
          onclick={finish}
          class="rounded-full bg-black/50 px-3 py-1.5 text-xs font-semibold text-white active:bg-black/70"
        >
          Finish ({seen.size}/{total})
        </button>
      {/snippet}
    </ScannerCamera>

    <!-- Verdict card: full-tint + icon + text (never color alone). Sits above
         the hold-to-scan button (which owns the bottom ~7rem of the viewport). -->
    {#if card}
      {#key scanPulse.seq}
      <div class="scan-card absolute inset-x-0 bottom-32 z-20 px-3">
        {#if card.kind === 'match'}
          <div class="rounded-2xl bg-emerald-950/95 border border-emerald-600 p-4">
            <div class="flex items-start gap-3">
              <span class="text-3xl leading-none">✓</span>
              <div class="min-w-0 flex-1">
                <p class="text-base font-bold text-emerald-300">In {targetRef}</p>
                <p class="font-mono text-sm font-semibold truncate mt-0.5">{card.entry.mpn ?? '—'}</p>
                <p class="text-xs text-emerald-200/80 mt-0.5">
                  {#if card.entry.qty_needed != null}need {card.entry.qty_needed.toLocaleString()}{/if}
                  {#if card.entry.designators} · {card.entry.designators}{/if}
                  {#if card.entry.order_ref} · {card.entry.order_ref}{/if}
                  {#if card.entry.note} · {card.entry.note}{/if}
                </p>
              </div>
            </div>
          </div>
        {:else if card.kind === 'substitute'}
          <div class="rounded-2xl bg-amber-950/95 border border-amber-600 p-4">
            <div class="flex items-start gap-3">
              <span class="text-3xl leading-none">≈</span>
              <div class="min-w-0 flex-1">
                <p class="text-base font-bold text-amber-300">Approved substitute</p>
                <p class="font-mono text-sm font-semibold truncate mt-0.5">{card.entry.mpn ?? '—'}</p>
                {#if card.entry.substitute_for}
                  <p class="text-xs text-amber-200/80 mt-0.5">substitutes {card.entry.substitute_for}</p>
                {/if}
              </div>
            </div>
          </div>
        {:else if card.kind === 'miss'}
          <div class="rounded-2xl bg-red-950/95 border border-red-700 p-4">
            <div class="flex items-start gap-3">
              <span class="text-3xl leading-none">✗</span>
              <div class="min-w-0 flex-1">
                <p class="text-base font-bold text-red-300">Not in {targetRef}</p>
                {#if card.mpn}
                  <p class="font-mono text-sm font-semibold truncate mt-0.5">{card.mpn}</p>
                {/if}
                {#if card.description}
                  <p class="text-xs text-red-200/70 truncate mt-0.5">{card.description}</p>
                {/if}
              </div>
            </div>
          </div>
        {:else}
          <div class="rounded-2xl bg-zinc-800/95 border border-zinc-600 p-4">
            <div class="flex items-center gap-3">
              <span class="text-3xl leading-none">?</span>
              <p class="text-sm font-semibold text-zinc-300">Unknown barcode — can't tell</p>
            </div>
          </div>
        {/if}
      </div>
      {/key}
    {/if}

    {#if confirmLeave}
      <div class="absolute inset-0 z-50 flex items-center justify-center bg-black/80 px-6">
        <div class="w-full max-w-sm rounded-2xl bg-zinc-900 p-6 space-y-4">
          <h2 class="text-base font-semibold">Leave the filter session?</h2>
          <p class="text-sm text-zinc-400">{seen.size} of {total} parts seen so far. The tally is not saved.</p>
          <div class="flex gap-3">
            <button
              onclick={() => (confirmLeave = false)}
              class="flex-1 rounded-lg border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
            >
              Keep scanning
            </button>
            <button
              onclick={() => { confirmLeave = false; onBack(); }}
              class="flex-1 rounded-lg bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
            >
              Leave
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>

<!-- ── SUMMARY ─────────────────────────────────────────────────────────────── -->
{:else if screen === 'summary'}
  <!-- Header, tally, and Done stay fixed; only the never-scanned list scrolls
       (same structure as the menu's Recent history). -->
  <div class="flex flex-1 flex-col overflow-hidden">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
      <p class="font-medium">Filter summary</p>
    </div>

    <div class="p-4 shrink-0">
      <div class="rounded-xl bg-zinc-900 p-4">
        <p class="text-2xl font-bold tabular-nums">{seen.size} / {total}</p>
        <p class="text-xs text-zinc-400 mt-0.5">parts of {targetRef} seen{missCount > 0 ? ` · ${missCount} didn't belong` : ''}{unknownCount > 0 ? ` · ${unknownCount} unknown` : ''}</p>
      </div>
    </div>

    {#if neverScanned.length > 0}
      <p class="px-4 pb-2 text-xs font-medium text-zinc-500 uppercase tracking-wide shrink-0">
        Never scanned ({neverScanned.length})
      </p>
      <div class="min-h-0 flex-1 overflow-y-auto px-4 pb-4 space-y-1.5">
        {#each neverScanned as e (e.part_id)}
          <div class="flex items-baseline justify-between gap-3 rounded-lg bg-zinc-900 px-3 py-2">
            <div class="min-w-0">
              <p class="font-mono text-sm truncate">{e.mpn ?? '—'}</p>
              {#if e.designators}
                <p class="text-[11px] text-zinc-500 truncate">{e.designators}</p>
              {/if}
            </div>
            {#if e.qty_needed != null}
              <span class="shrink-0 text-xs text-zinc-400 tabular-nums">need {e.qty_needed.toLocaleString()}</span>
            {/if}
          </div>
        {/each}
      </div>
    {:else}
      <p class="px-4 text-sm text-emerald-400 shrink-0">Everything in {targetRef} was seen. ✓</p>
      <div class="flex-1"></div>
    {/if}

    <div class="p-4 border-t border-zinc-800 shrink-0">
      <button
        onclick={onBack}
        class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Done
      </button>
    </div>
  </div>
{/if}

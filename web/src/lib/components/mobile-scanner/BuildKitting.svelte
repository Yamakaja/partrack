<script lang="ts">
  import { onMount } from 'svelte';
  import {
    getKittingSession,
    getKittingItems,
    kittingConfirm,
    kittingShortfall,
    kittingComplete,
    kittingAbandon,
    ScanExpiredError,
    type KittingSession,
    type KittingItem,
    type KittingProgress,
  } from '$lib/api/scanner.js';
  import { fullLocationLabel } from './locationUtils.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import type { StorageLocation } from '$lib/types/inventory.js';

  let {
    kittingSessionId,
    buildRef,
    locations,
    onBack,
    onExpired,
  }: {
    kittingSessionId: string;
    buildRef: string;
    locations: StorageLocation[];
    onBack: () => void;
    onExpired: () => void;
  } = $props();

  // ── Data ──────────────────────────────────────────────────────────────────

  let session = $state<KittingSession | null>(null);
  let items = $state<KittingItem[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);

  // ── Item interaction ──────────────────────────────────────────────────────

  let activeItemId = $state<string | null>(null);
  let shortfallInput = $state('');
  let submitting = $state(false);

  const pendingItems = $derived(items.filter((i) => i.status === 'pending'));
  const confirmedItems = $derived(items.filter((i) => i.status === 'confirmed'));
  const shortfallItems = $derived(items.filter((i) => i.status === 'shortfall'));
  const skippedItems = $derived(items.filter((i) => i.status === 'skipped'));
  const doneItems = $derived([...confirmedItems, ...shortfallItems, ...skippedItems]);
  const progress = $derived(session?.progress ?? { pending: 0, confirmed: 0, shortfall: 0, skipped: 0 });
  const total = $derived(progress.pending + progress.confirmed + progress.shortfall + progress.skipped);
  const done = $derived(progress.confirmed + progress.shortfall + progress.skipped);
  const allDone = $derived(total > 0 && done === total);

  async function load() {
    try {
      const [s, its] = await Promise.all([
        getKittingSession(kittingSessionId),
        getKittingItems(kittingSessionId),
      ]);
      session = s;
      items = its.items;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      error = 'Failed to load kitting session';
    } finally {
      loading = false;
    }
  }

  async function confirm(item: KittingItem) {
    if (submitting) return;
    submitting = true;
    try {
      const res = await kittingConfirm(kittingSessionId, item.part_id, item.quantity_needed);
      session = { ...session!, progress: res.progress };
      items = items.map((i) =>
        i.part_id === item.part_id
          ? { ...i, status: 'confirmed', quantity_confirmed: item.quantity_needed }
          : i,
      );
      activeItemId = null;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not confirm', 'Please try again.');
    } finally {
      submitting = false;
    }
  }

  async function submitShortfall(item: KittingItem) {
    const qty = parseInt(shortfallInput, 10);
    if (isNaN(qty) || qty < 0) return;
    if (submitting) return;
    submitting = true;
    try {
      const res = await kittingShortfall(kittingSessionId, item.part_id, qty);
      session = { ...session!, progress: res.progress };
      items = items.map((i) =>
        i.part_id === item.part_id
          ? { ...i, status: 'shortfall', quantity_confirmed: qty }
          : i,
      );
      activeItemId = null;
      shortfallInput = '';
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not record shortfall', 'Please try again.');
    } finally {
      submitting = false;
    }
  }

  async function complete() {
    if (submitting) return;
    submitting = true;
    try {
      await kittingComplete(kittingSessionId);
      pushToast('success', 'Kitting complete', 'Check out the build on desktop to record assembly.');
      onBack();
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not complete kitting', 'Please try again.');
    } finally {
      submitting = false;
    }
  }

  async function abandon() {
    if (submitting) return;
    submitting = true;
    try {
      await kittingAbandon(kittingSessionId);
      onBack();
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not abandon kitting', 'Please try again.');
    } finally {
      submitting = false;
    }
  }

  function locationLabel(item: KittingItem): string | null {
    if (!item.storage_location_id) return null;
    const loc = locations.find((l) => l.id === item.storage_location_id);
    if (loc) return fullLocationLabel({ id: item.storage_location_id, coordinate: item.storage_coordinate }, locations);
    return item.storage_location_path ?? null;
  }

  onMount(load);
</script>

<!-- Header -->
<div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
  <button onclick={onBack} aria-label="Back" class="text-zinc-400 hover:text-white transition-colors">
    <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
    </svg>
  </button>
  <div class="flex-1 min-w-0">
    <p class="text-sm font-semibold truncate">Kitting: {buildRef}</p>
    {#if !loading && total > 0}
      <p class="text-xs text-zinc-500">{done} / {total} done</p>
    {/if}
  </div>
  <button
    onclick={abandon}
    disabled={submitting}
    class="text-xs text-zinc-600 hover:text-red-400 transition-colors"
  >
    Abandon
  </button>
</div>

<!-- Progress bar -->
{#if !loading && total > 0}
  <div class="h-1 bg-zinc-800 shrink-0">
    <div
      class="h-full bg-emerald-500 transition-all"
      style="width: {Math.round((done / total) * 100)}%"
    ></div>
  </div>
{/if}

<!-- Body -->
<div class="flex-1 overflow-y-auto">
  {#if loading}
    <div class="flex items-center justify-center p-12">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
    </div>
  {:else if error}
    <div class="p-6 text-center text-sm text-red-400">{error}</div>
  {:else if allDone}
    <!-- Summary -->
    <div class="flex flex-col items-center gap-4 p-8 text-center">
      <div class="text-4xl">{shortfallItems.length === 0 ? '✓' : '⚠'}</div>
      <div>
        <p class="text-base font-semibold">
          {shortfallItems.length === 0 ? 'All parts confirmed!' : 'Kitting complete with shortfalls'}
        </p>
        <p class="mt-1 text-sm text-zinc-400">
          {confirmedItems.length} confirmed · {shortfallItems.length} shortfall
        </p>
      </div>
      <button
        onclick={complete}
        disabled={submitting}
        class="mt-2 w-full rounded-xl bg-emerald-600 py-4 text-sm font-semibold hover:bg-emerald-500 disabled:opacity-50 transition-colors"
      >
        {submitting ? 'Completing…' : 'Complete Kitting'}
      </button>
    </div>

    <!-- Done items recap -->
    {#each doneItems as item (item.part_id)}
      <div class="flex items-center gap-3 px-4 py-3 border-t border-zinc-900">
        <div class="shrink-0 w-6 h-6 rounded-full flex items-center justify-center text-xs
          {item.status === 'confirmed' ? 'bg-emerald-900/50 text-emerald-400' : 'bg-amber-900/50 text-amber-400'}">
          {item.status === 'confirmed' ? '✓' : '⚠'}
        </div>
        <div class="flex-1 min-w-0">
          <p class="text-xs font-mono truncate">{item.part_mpn ?? item.part_id}</p>
          <p class="text-[10px] text-zinc-600">{item.quantity_confirmed} / {item.quantity_needed}</p>
        </div>
      </div>
    {/each}
  {:else}
    <!-- Pending items -->
    {#each pendingItems as item (item.part_id)}
      {@const loc = locationLabel(item)}
      {@const isActive = activeItemId === item.part_id}
      <div class="border-b border-zinc-900">
        <!-- Item row -->
        <button
          onclick={() => { activeItemId = isActive ? null : item.part_id; shortfallInput = ''; }}
          class="w-full flex gap-3 px-4 py-4 text-left hover:bg-zinc-900/50 transition-colors"
        >
          <div class="shrink-0 w-7 h-7 rounded-full bg-zinc-800 flex items-center justify-center text-xs text-zinc-400">
            ·
          </div>
          <div class="flex-1 min-w-0">
            <p class="text-sm font-mono truncate">{item.part_mpn ?? item.part_id}</p>
            {#if item.part_description}
              <p class="text-xs text-zinc-500 truncate mt-0.5">{item.part_description}</p>
            {/if}
            <div class="flex items-center gap-3 mt-1">
              <span class="text-xs text-zinc-400">Qty: <span class="font-semibold text-white">{item.quantity_needed}</span></span>
              {#if loc}
                <span class="text-xs text-zinc-500 truncate">{loc}</span>
              {/if}
            </div>
          </div>
          <svg class="shrink-0 h-4 w-4 text-zinc-600 self-center transition-transform {isActive ? 'rotate-90' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
          </svg>
        </button>

        <!-- Actions (expanded) -->
        {#if isActive}
          <div class="px-4 pb-4 space-y-3">
            <button
              onclick={() => confirm(item)}
              disabled={submitting}
              class="w-full rounded-xl bg-emerald-700 py-3 text-sm font-semibold hover:bg-emerald-600 disabled:opacity-50 transition-colors"
            >
              Confirm ({item.quantity_needed})
            </button>

            <div class="flex gap-2 items-center">
              <input
                type="number"
                min="0"
                max={item.quantity_needed}
                inputmode="numeric"
                placeholder="Available qty"
                bind:value={shortfallInput}
                class="flex-1 rounded-lg bg-zinc-800 border border-zinc-700 px-3 py-2 text-sm text-white placeholder:text-zinc-600 focus:outline-none focus:border-amber-500"
              />
              <button
                onclick={() => submitShortfall(item)}
                disabled={submitting || shortfallInput === ''}
                class="rounded-xl bg-amber-700 px-4 py-2 text-sm font-semibold hover:bg-amber-600 disabled:opacity-50 transition-colors"
              >
                Shortfall
              </button>
            </div>
          </div>
        {/if}
      </div>
    {/each}

    <!-- Done items (collapsed) -->
    {#if doneItems.length > 0}
      <div class="px-4 pt-4 pb-2">
        <p class="text-xs font-medium text-zinc-600 uppercase tracking-wide">Done ({doneItems.length})</p>
      </div>
      {#each doneItems as item (item.part_id)}
        <div class="flex items-center gap-3 px-4 py-3 border-t border-zinc-900 opacity-60">
          <div class="shrink-0 w-6 h-6 rounded-full flex items-center justify-center text-xs
            {item.status === 'confirmed' ? 'bg-emerald-900/50 text-emerald-400' : 'bg-amber-900/50 text-amber-400'}">
            {item.status === 'confirmed' ? '✓' : '⚠'}
          </div>
          <div class="flex-1 min-w-0">
            <p class="text-xs font-mono truncate">{item.part_mpn ?? item.part_id}</p>
            <p class="text-[10px] text-zinc-600">{item.quantity_confirmed} / {item.quantity_needed}</p>
          </div>
        </div>
      {/each}
    {/if}
  {/if}
</div>

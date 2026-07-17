<script lang="ts">
  import { onMount } from 'svelte';
  import ScannerCamera from './ScannerCamera.svelte';
  import {
    listScanCollections, createScanCollection, patchScanCollection, collectScan,
    listScanCollectionItems,
    startBulkSession, addBulkScanItem, closeBulkSession,
    ScanExpiredError,
    type ScanCollection, type CollectPart, type CollectTally, type ScanCollectionItem,
  } from '$lib/api/scanner.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { fullLocationLabel } from './locationUtils.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

  // The phone's Lists surface: browse a list (rows + stock + storage location —
  // the "where do these live?" lens) and collect into it. Collect is strictly
  // list-writes only — no inventory is touched (stock only ever moves at
  // receive/checkout).
  let {
    locations = [],
    initialCollectionId = null,
    initialCollectionName = null,
    onBack,
    onExpired,
  }: {
    /** Cached storage tree (for location labels). */
    locations?: StorageLocation[];
    /** Pre-selected list (desktop "Collect on phone" push) — skips the browse step. */
    initialCollectionId?: string | null;
    initialCollectionName?: string | null;
    onBack: () => void;
    onExpired: () => void;
  } = $props();

  type Selected = { id: string; name: string; captureQty: boolean | null };

  let screen = $state<'picker' | 'detail' | 'scanning'>('picker');
  let lists = $state<ScanCollection[]>([]);
  let loadingLists = $state(true);
  let selected = $state<Selected | null>(null);
  let detailItems = $state<ScanCollectionItem[]>([]);
  let detailLoading = $state(false);

  // Capture-quantities decision dialog (first collect into a list).
  let showCaptureDialog = $state(false);

  // Running tally (server-authoritative after each add).
  let tally = $state<CollectTally | null>(null);
  let unknownCount = $state(0);

  // Result card (persists until the next scan, filter-spec convention).
  type ResultCard =
    | { kind: 'added'; part: CollectPart; quantityAdded: number | null; rowQuantity: number | null }
    | { kind: 'unknown'; raw: string; label: string; sentToResolve: boolean }
    | { kind: 'error'; message: string };
  let card = $state<ResultCard | null>(null);

  // Quantity prompt (capture ON, barcode carried no qty). Skippable — skip
  // records bare membership; we never invent a quantity.
  let qtyPrompt = $state<CollectPart | null>(null);
  let qtyInput = $state('');
  let busy = $state(false);

  // Lazy bulk session for unknown-bag escape hatch ("resolve leftovers").
  let leftoverSessionId = $state<string | null>(null);

  // Debounce identical raw barcodes (camera re-fires while held on the bag).
  const seenBarcodes = new Map<string, number>();

  function focusOnMount(node: HTMLElement) {
    node.focus();
  }

  onMount(() => {
    void refreshLists();
    return () => {
      // A leftovers session is desktop-reviewable; close it so it shows up
      // as ready for review instead of lingering active.
      if (leftoverSessionId) closeBulkSession(leftoverSessionId).catch(() => {});
    };
  });

  async function refreshLists() {
    loadingLists = true;
    try {
      const res = await listScanCollections();
      lists = res.collections;
      // Desktop push preselects and goes straight to scanning; wait for the
      // fetch so we know the capture toggle.
      if (initialCollectionId && !selected) {
        const match = lists.find((l) => l.id === initialCollectionId);
        if (match) {
          startCollect(match);
        } else if (initialCollectionName) {
          pushToast('warning', `List “${initialCollectionName}” not found`);
        }
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not load lists');
    } finally {
      loadingLists = false;
    }
  }

  // Browse: tap a list → detail view (rows + stock + locations).
  async function pick(l: ScanCollection) {
    selected = { id: l.id, name: l.name, captureQty: l.collect_capture_qty };
    tally = { item_count: l.item_count, total_quantity: l.total_quantity, unquantified_count: 0 };
    card = null;
    unknownCount = 0;
    screen = 'detail';
    detailLoading = true;
    detailItems = [];
    try {
      detailItems = (await listScanCollectionItems(l.id)).items;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not load the list');
    } finally {
      detailLoading = false;
    }
  }

  // Start the scan loop (from the detail button or a desktop push).
  function startCollect(l: ScanCollection) {
    selected = { id: l.id, name: l.name, captureQty: l.collect_capture_qty };
    tally = { item_count: l.item_count, total_quantity: l.total_quantity, unquantified_count: 0 };
    card = null;
    unknownCount = 0;
    if (l.collect_capture_qty === null) {
      showCaptureDialog = true;
    } else {
      screen = 'scanning';
    }
  }

  function startCollectFromDetail() {
    if (!selected) return;
    if (selected.captureQty === null) {
      showCaptureDialog = true;
    } else {
      screen = 'scanning';
    }
  }

  async function newList() {
    try {
      const c = await createScanCollection();
      selected = { id: c.id, name: c.name, captureQty: null };
      tally = { item_count: 0, total_quantity: 0, unquantified_count: 0 };
      unknownCount = 0;
      showCaptureDialog = true;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not create list');
    }
  }

  function itemLocationLabel(it: ScanCollectionItem): string | null {
    if (!it.storage_location) return null;
    return fullLocationLabel(it.storage_location, locations) || null;
  }

  // Leaving the scan loop lands on the (refreshed) detail view.
  async function backToDetail() {
    card = null;
    screen = 'detail';
    detailLoading = true;
    void refreshLists();
    try {
      if (selected) detailItems = (await listScanCollectionItems(selected.id)).items;
    } catch {
      // list view is best-effort here; scan writes already succeeded
    } finally {
      detailLoading = false;
    }
  }

  async function decideCapture(capture: boolean) {
    showCaptureDialog = false;
    if (!selected) return;
    selected = { ...selected, captureQty: capture };
    screen = 'scanning';
    try {
      await patchScanCollection(selected.id, capture);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      // Non-fatal: the server treats an unset toggle as capture-off.
      pushToast('warning', 'Could not save the quantity setting');
    }
  }

  // ── Scan loop ─────────────────────────────────────────────────────────────

  async function handleScan(raw: string) {
    if (!selected || busy || qtyPrompt) return;
    const now = Date.now();
    const last = seenBarcodes.get(raw);
    seenBarcodes.set(raw, now);
    if (last !== undefined && now - last <= 2500) return;

    busy = true;
    try {
      const res = await collectScan({ collection_id: selected.id, barcode: raw });
      if (res.status === 'added') {
        card = {
          kind: 'added',
          part: res.part,
          quantityAdded: res.quantity_added,
          rowQuantity: res.item_quantity,
        };
        tally = res.tally;
      } else if (res.status === 'quantity_required') {
        qtyPrompt = res.part;
        qtyInput = '';
      } else {
        card = {
          kind: 'unknown',
          raw,
          label: res.parsed_sku || res.parsed_mpn || 'unrecognised barcode',
          sentToResolve: false,
        };
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      card = { kind: 'error', message: 'Scan failed — try again' };
    } finally {
      busy = false;
    }
  }

  // Second phase after the quantity prompt: confirm with a count, or skip to
  // record bare membership.
  async function submitPromptedQty(quantity: number | null) {
    const part = qtyPrompt;
    qtyPrompt = null;
    if (!selected || !part) return;
    busy = true;
    try {
      const res = await collectScan({
        collection_id: selected.id,
        part_id: part.id,
        ...(quantity != null ? { quantity } : {}),
      });
      if (res.status === 'added') {
        card = {
          kind: 'added',
          part: res.part,
          quantityAdded: res.quantity_added,
          rowQuantity: res.item_quantity,
        };
        tally = res.tally;
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      card = { kind: 'error', message: 'Could not add — try again' };
    } finally {
      busy = false;
    }
  }

  function handleQtyConfirm() {
    const n = parseInt(qtyInput, 10);
    if (isNaN(n) || n <= 0) {
      submitPromptedQty(null);
    } else {
      submitPromptedQty(n);
    }
  }

  // ── Unknown-bag escape hatch: hand off to the bulk-resolve pipeline ──────

  async function sendToResolveQueue() {
    if (!card || card.kind !== 'unknown' || card.sentToResolve) return;
    const raw = card.raw;
    try {
      if (!leftoverSessionId) {
        const res = await startBulkSession();
        leftoverSessionId = res.session_id;
      }
      await addBulkScanItem(leftoverSessionId, raw);
      unknownCount += 1;
      card = { ...card, sentToResolve: true };
      pushToast('success', 'Queued for resolution — review on the desktop');
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not queue the barcode');
    }
  }
</script>

<!-- ── LIST PICKER ─────────────────────────────────────────────────────────── -->
{#if screen === 'picker'}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Collect into…</p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-2">
      {#if loadingLists}
        <p class="text-sm text-zinc-500 text-center py-6">Loading lists…</p>
      {:else}
        {#each lists as l (l.id)}
          <button
            onclick={() => pick(l)}
            class="w-full flex items-center justify-between gap-3 rounded-xl bg-zinc-900 px-4 py-3.5 text-left active:bg-zinc-800"
          >
            <div class="min-w-0">
              <p class="font-medium truncate">{l.name}</p>
              <p class="text-xs text-zinc-400">
                {l.item_count} part{l.item_count === 1 ? '' : 's'}{l.total_quantity > 0 ? ` · ${l.total_quantity.toLocaleString()} pcs` : ''}
              </p>
            </div>
            <svg class="h-4 w-4 shrink-0 text-zinc-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
            </svg>
          </button>
        {:else}
          <p class="text-sm text-zinc-500 text-center py-6">No lists yet — create one below.</p>
        {/each}

        <button
          onclick={newList}
          class="w-full rounded-xl border border-dashed border-zinc-700 px-4 py-3.5 text-sm font-semibold text-zinc-300 active:bg-zinc-900"
        >
          + New list
        </button>
      {/if}
    </div>
  </div>

<!-- ── LIST DETAIL (browse: stock + locations) ─────────────────────────────── -->
{:else if screen === 'detail' && selected}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => { screen = 'picker'; void refreshLists(); }} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <div class="min-w-0">
        <p class="font-medium truncate">{selected.name}</p>
        {#if tally}
          <p class="text-xs text-zinc-400">
            {tally.item_count} part{tally.item_count === 1 ? '' : 's'}{tally.total_quantity > 0 ? ` · ${tally.total_quantity.toLocaleString()} pcs` : ''}
          </p>
        {/if}
      </div>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-1.5">
      {#if detailLoading}
        <p class="text-sm text-zinc-500 text-center py-6">Loading…</p>
      {:else}
        {#each detailItems as it (it.id)}
          {@const loc = itemLocationLabel(it)}
          <div class="rounded-lg bg-zinc-900 px-3 py-2.5">
            <div class="flex items-baseline justify-between gap-3">
              <p class="min-w-0 truncate font-mono text-sm font-medium">{it.mpn ?? it.description ?? '—'}</p>
              <span class="shrink-0 text-xs tabular-nums {it.quantity == null ? 'text-amber-400' : 'text-zinc-300'}">
                {it.quantity != null ? `× ${it.quantity.toLocaleString()}` : 'no qty'}
              </span>
            </div>
            {#if it.mpn && it.description}
              <p class="text-xs text-zinc-500 truncate">{it.description}</p>
            {/if}
            <div class="mt-1 flex items-baseline justify-between gap-3 text-[11px]">
              <span class="min-w-0 truncate text-zinc-400">
                {#if loc}📍 {loc}{:else}<span class="text-zinc-600">no location</span>{/if}
              </span>
              <span class="shrink-0 tabular-nums {it.quantity_on_hand != null && it.quantity_on_hand > 0 ? 'text-zinc-400' : 'text-zinc-600'}">
                {it.quantity_on_hand != null ? `${it.quantity_on_hand.toLocaleString()} on hand` : 'not tracked'}
              </span>
            </div>
            {#if it.note}
              <p class="mt-0.5 text-[11px] italic text-zinc-500 truncate">{it.note}</p>
            {/if}
          </div>
        {:else}
          <p class="text-sm text-zinc-500 text-center py-6">Empty list — start collecting below.</p>
        {/each}
      {/if}
    </div>

    <div class="p-4 border-t border-zinc-800">
      <button
        onclick={startCollectFromDetail}
        class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Collect into this list
      </button>
    </div>
  </div>

<!-- ── SCANNING ────────────────────────────────────────────────────────────── -->
{:else if selected}
  <div class="relative flex flex-1 flex-col overflow-hidden">
    <ScannerCamera onBack={backToDetail} onScan={handleScan} label={`Collect: ${selected.name}`}>
      {#snippet topRightControls()}
        {#if tally}
          <div class="rounded-full bg-black/50 px-3 py-1.5 text-xs font-semibold text-white">
            {tally.item_count} · {tally.total_quantity.toLocaleString()} pcs{unknownCount > 0 ? ` · ${unknownCount} ❓` : ''}
          </div>
        {/if}
      {/snippet}
    </ScannerCamera>

    <!-- Result card: persists until the next scan. Sits above the
         hold-to-scan button (which owns the bottom ~7rem of the viewport). -->
    {#if card && !qtyPrompt}
      <div class="absolute inset-x-0 bottom-32 z-20 px-3">
        {#if card.kind === 'added'}
          <div class="rounded-2xl bg-emerald-950/95 border border-emerald-700 p-4">
            <div class="flex items-center gap-3">
              <span class="text-2xl">✓</span>
              <div class="min-w-0 flex-1">
                <p class="font-mono text-sm font-semibold truncate">{card.part.mpn ?? '—'}</p>
                {#if card.part.description}
                  <p class="text-xs text-emerald-200/80 truncate">{card.part.description}</p>
                {/if}
              </div>
              <div class="shrink-0 text-right">
                {#if card.quantityAdded != null}
                  <p class="text-lg font-bold tabular-nums">+{card.quantityAdded.toLocaleString()}</p>
                  {#if card.rowQuantity != null && card.rowQuantity !== card.quantityAdded}
                    <p class="text-[11px] text-emerald-300/80 tabular-nums">row: {card.rowQuantity.toLocaleString()}</p>
                  {/if}
                {:else}
                  <p class="text-sm font-semibold">on the list</p>
                {/if}
              </div>
            </div>
          </div>
        {:else if card.kind === 'unknown'}
          <div class="rounded-2xl bg-zinc-900/95 border border-zinc-600 p-4 space-y-3">
            <div class="flex items-center gap-3">
              <span class="text-2xl">❓</span>
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold">Not in the catalog</p>
                <p class="text-xs font-mono text-zinc-400 truncate">{card.label}</p>
              </div>
            </div>
            {#if card.sentToResolve}
              <p class="text-xs text-zinc-400">Queued for resolution — it is <span class="font-semibold">not</span> on the list. Re-scan after importing.</p>
            {:else}
              <button
                onclick={sendToResolveQueue}
                class="w-full rounded-lg bg-white py-2.5 text-sm font-semibold text-black active:bg-zinc-200"
              >
                Send to resolve queue
              </button>
            {/if}
          </div>
        {:else}
          <div class="rounded-2xl bg-red-950/95 border border-red-800 p-4">
            <p class="text-sm font-semibold">{card.message}</p>
          </div>
        {/if}
      </div>
    {/if}

    <!-- Quantity prompt (capture ON, barcode had no qty) — skippable -->
    {#if qtyPrompt}
      <div class="absolute inset-x-0 bottom-0 z-30 rounded-t-2xl bg-zinc-900 p-6 space-y-4">
        <div>
          <p class="text-sm font-semibold">How many pieces?</p>
          <p class="mt-0.5 text-xs font-mono text-zinc-400 truncate">{qtyPrompt.mpn ?? qtyPrompt.description ?? ''}</p>
        </div>
        <input
          type="number"
          min="1"
          inputmode="numeric"
          bind:value={qtyInput}
          placeholder="Enter quantity"
          onkeydown={(e) => { if (e.key === 'Enter') handleQtyConfirm(); }}
          class="w-full rounded-lg bg-zinc-800 px-4 py-3 text-base text-white placeholder-zinc-500 focus:outline-none"
          use:focusOnMount
        />
        <div class="flex gap-3">
          <button
            onclick={() => submitPromptedQty(null)}
            class="flex-1 rounded-lg border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
          >
            Skip — no count
          </button>
          <button
            onclick={handleQtyConfirm}
            class="flex-1 rounded-lg bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
          >
            Add
          </button>
        </div>
      </div>
    {/if}
  </div>
{/if}

<!-- ── CAPTURE-QUANTITIES DIALOG (first collect into this list) ────────────── -->
{#if showCaptureDialog && selected}
  <div class="absolute inset-0 z-50 flex items-center justify-center bg-black/80 px-6">
    <div class="w-full max-w-sm rounded-2xl bg-zinc-900 p-6 space-y-4">
      <h2 class="text-base font-semibold">Capture quantities?</h2>
      <p class="text-sm text-zinc-400">
        Record piece counts while scanning into “{selected.name}”? Barcodes that carry a
        count use it automatically; otherwise you'll be asked. Saved for this list.
      </p>
      <div class="flex gap-3">
        <button
          onclick={() => decideCapture(false)}
          class="flex-1 rounded-lg border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
        >
          Just parts
        </button>
        <button
          onclick={() => decideCapture(true)}
          class="flex-1 rounded-lg bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
        >
          With counts
        </button>
      </div>
    </div>
  </div>
{/if}

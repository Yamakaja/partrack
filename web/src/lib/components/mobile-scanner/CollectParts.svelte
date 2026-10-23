<script lang="ts">
  import { onMount } from 'svelte';
  import ScannerCamera from './ScannerCamera.svelte';
  import {
    listScanCollections, createScanCollection, patchScanCollection, collectScan,
    listScanCollectionItems, pickScan, setScanCollectionChecked,
    startBulkSession, addBulkScanItem, closeBulkSession,
    ScanExpiredError,
    type ScanCollection, type CollectPart, type CollectTally, type ScanCollectionItem,
  } from '$lib/api/scanner.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { fullLocationLabel } from './locationUtils.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { scanFeedback, scanPulse } from './feedback.svelte.js';

  // The phone's Lists surface. Three things happen here:
  //   browse — a list's rows with stock and storage ("where do these live?")
  //   collect — scan bags INTO the list
  //   pick   — sort an unsorted pile ON the list, ticking rows off as you find
  //            them; the ticks are the same persisted selection the desktop
  //            panel shows as checkboxes, so the sorted pile is waiting there.
  // Both scan modes are strictly list-writes — no inventory is touched (stock
  // only ever moves at receive/checkout).
  let {
    locations = [],
    initialCollectionId = null,
    initialCollectionName = null,
    initialMode = 'collect',
    onBack,
    onExpired,
  }: {
    /** Cached storage tree (for location labels). */
    locations?: StorageLocation[];
    /** Pre-selected list (desktop "Collect/Pick on phone" push) — skips the browse step. */
    initialCollectionId?: string | null;
    initialCollectionName?: string | null;
    initialMode?: 'collect' | 'pick';
    onBack: () => void;
    onExpired: () => void;
  } = $props();

  type Selected = { id: string; name: string; captureQty: boolean | null };

  let screen = $state<'picker' | 'detail' | 'scanning' | 'picking'>('picker');
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

  // ── Pick mode ─────────────────────────────────────────────────────────────
  // Progress is derived from detailItems rather than tracked separately: each
  // scan returns the row it ticked, so the sheet and the tally stay in step
  // without re-fetching the list per bag.
  type PickCard =
    | { kind: 'checked'; part: CollectPart | null; quantity: number | null; note: string | null }
    | { kind: 'already'; part: CollectPart | null }
    | { kind: 'not_on_list'; part: CollectPart }
    | { kind: 'unknown'; label: string }
    | { kind: 'error'; message: string };
  let pickCard = $state<PickCard | null>(null);
  let sheetOpen = $state(false);
  // Set when a scan (not a manual tap) ticked a row — the undo target.
  let lastPickedItemId = $state<string | null>(null);
  // Raised on entry when the list still carries earlier ticks; continuing a
  // half-done run is the point, but a stale one would make the count a lie.
  let showStartFresh = $state(false);

  const pickTotal = $derived(detailItems.length);
  const pickDone = $derived(detailItems.filter((i) => i.checked_at !== null).length);
  const outstanding = $derived(detailItems.filter((i) => i.checked_at === null));

  function applyChecked(itemId: string, checkedAt: string | null) {
    detailItems = detailItems.map((i) => (i.id === itemId ? { ...i, checked_at: checkedAt } : i));
  }

  function startPickFromDetail() {
    if (!selected) return;
    pickCard = null;
    lastPickedItemId = null;
    sheetOpen = false;
    screen = 'picking';
    // The resume prompt rides on top of the (already mounted) camera screen.
    if (pickDone > 0) showStartFresh = true;
  }

  async function startFresh(fresh: boolean) {
    showStartFresh = false;
    screen = 'picking';
    if (!fresh || !selected) return;
    try {
      await setScanCollectionChecked(selected.id, 'none');
      detailItems = detailItems.map((i) => ({ ...i, checked_at: null }));
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('warning', 'Could not clear the earlier ticks');
    }
  }

  async function handlePickScan(raw: string) {
    if (!selected || busy) return;
    const now = Date.now();
    const last = seenBarcodes.get(raw);
    seenBarcodes.set(raw, now);
    if (last !== undefined && now - last <= 2500) { scanFeedback('notice'); return; }

    busy = true;
    try {
      const res = await pickScan(selected.id, { barcode: raw });
      if (res.status === 'checked') {
        applyChecked(res.item_id, res.checked_at);
        lastPickedItemId = res.item_id;
        pickCard = {
          kind: 'checked',
          part: res.part ?? null,
          quantity: res.quantity ?? null,
          note: res.note ?? null,
        };
        scanFeedback(pickDone === pickTotal ? 'complete' : 'success');
      } else if (res.status === 'already_checked') {
        applyChecked(res.item_id, res.checked_at);
        lastPickedItemId = null;
        pickCard = { kind: 'already', part: res.part ?? null };
        scanFeedback('notice');
      } else if (res.status === 'not_on_list') {
        lastPickedItemId = null;
        pickCard = { kind: 'not_on_list', part: res.part };
        scanFeedback('error');
      } else if (res.status === 'unknown') {
        lastPickedItemId = null;
        pickCard = { kind: 'unknown', label: res.parsed_sku || res.parsed_mpn || 'unrecognised barcode' };
        scanFeedback('error');
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pickCard = { kind: 'error', message: 'Scan failed — try again' };
      scanFeedback('error');
    } finally {
      busy = false;
    }
  }

  // Manual tick: cut tape, house-labelled bins and loose parts carry no
  // barcode, so the outstanding sheet is tappable.
  async function tickItem(it: ScanCollectionItem, checked: boolean) {
    if (!selected || busy) return;
    busy = true;
    try {
      const res = await pickScan(selected.id, { item_id: it.id, checked });
      if (res.status === 'checked' || res.status === 'unchecked') {
        applyChecked(res.item_id, res.checked_at);
        lastPickedItemId = null;
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not update the row');
    } finally {
      busy = false;
    }
  }

  async function undoLastPick() {
    const itemId = lastPickedItemId;
    if (!selected || !itemId || busy) return;
    busy = true;
    try {
      const res = await pickScan(selected.id, { item_id: itemId, checked: false });
      if (res.status === 'unchecked') applyChecked(res.item_id, res.checked_at);
      lastPickedItemId = null;
      pickCard = null;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not undo');
    } finally {
      busy = false;
    }
  }

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
          if (initialMode === 'pick') void pick(match, true);
          else startCollect(match);
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

  // Browse: tap a list → detail view (rows + stock + locations). A desktop Pick
  // push goes straight on to the scan loop once the rows are in, since pick
  // needs them loaded to have a target at all.
  async function pick(l: ScanCollection, thenPick = false) {
    selected = { id: l.id, name: l.name, captureQty: l.collect_capture_qty };
    tally = { item_count: l.item_count, total_quantity: l.total_quantity, unquantified_count: 0 };
    card = null;
    unknownCount = 0;
    screen = 'detail';
    detailLoading = true;
    detailItems = [];
    try {
      detailItems = (await listScanCollectionItems(l.id)).items;
      if (thenPick) startPickFromDetail();
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

  // Leaving either scan loop lands on the (refreshed) detail view.
  async function backToDetail() {
    card = null;
    pickCard = null;
    sheetOpen = false;
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
    if (last !== undefined && now - last <= 2500) { scanFeedback('notice'); return; }

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
        scanFeedback('success');
      } else if (res.status === 'quantity_required') {
        qtyPrompt = res.part;
        qtyInput = '';
        scanFeedback('warning');
      } else {
        card = {
          kind: 'unknown',
          raw,
          label: res.parsed_sku || res.parsed_mpn || 'unrecognised barcode',
          sentToResolve: false,
        };
        scanFeedback('error');
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      card = { kind: 'error', message: 'Scan failed — try again' };
      scanFeedback('error');
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
        scanFeedback('success');
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      card = { kind: 'error', message: 'Could not add — try again' };
      scanFeedback('error');
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
  <div class="flex flex-1 flex-col overflow-hidden">
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
                {l.item_count} part{l.item_count === 1 ? '' : 's'}{l.total_quantity > 0 ? ` · ${l.total_quantity.toLocaleString()} pcs` : ''}{l.checked_count > 0 ? ` · ${l.checked_count} ticked` : ''}
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
  <div class="flex flex-1 flex-col overflow-hidden">
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
            {tally.item_count} part{tally.item_count === 1 ? '' : 's'}{tally.total_quantity > 0 ? ` · ${tally.total_quantity.toLocaleString()} pcs` : ''}{pickDone > 0 ? ` · ${pickDone} ticked` : ''}
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
          <div class="rounded-lg px-3 py-2.5 {it.checked_at ? 'bg-emerald-950/60 border border-emerald-800/60' : 'bg-zinc-900'}">
            <div class="flex items-baseline justify-between gap-3">
              <p class="min-w-0 truncate font-mono text-sm font-medium">
                {#if it.checked_at}<span class="mr-1 text-emerald-400">✓</span>{/if}{it.mpn ?? it.description ?? '—'}
              </p>
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

    <div class="p-4 border-t border-zinc-800 space-y-2">
      <button
        onclick={startCollectFromDetail}
        class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Collect into this list
      </button>
      <button
        onclick={startPickFromDetail}
        disabled={detailItems.length === 0}
        class="w-full rounded-lg border border-zinc-700 py-3 text-base font-semibold text-zinc-200 active:bg-zinc-900 disabled:opacity-40"
      >
        Pick from a pile{pickTotal > 0 ? ` (${pickDone}/${pickTotal})` : ''}
      </button>
    </div>
  </div>

<!-- ── PICKING (sort a pile against the list, ticking rows off) ────────────── -->
{:else if screen === 'picking' && selected}
  <div class="relative flex flex-1 flex-col overflow-hidden">
    <ScannerCamera onBack={backToDetail} onScan={handlePickScan} label={`Pick: ${selected.name}`}>
      {#snippet topRightControls()}
        <button
          onclick={() => (sheetOpen = true)}
          class="rounded-full bg-black/50 px-3 py-1.5 text-xs font-semibold text-white active:bg-black/70"
        >
          {pickDone}/{pickTotal} ▸
        </button>
      {/snippet}
    </ScannerCamera>

    <!-- Verdict card: full tint + icon + text, never colour alone. Sits above
         the hold-to-scan button (which owns the bottom ~7rem of the viewport). -->
    {#if pickCard && !sheetOpen && !showStartFresh}
      {#key scanPulse.seq}
      <div class="scan-card absolute inset-x-0 bottom-32 z-20 px-3">
        {#if pickCard.kind === 'checked'}
          <div class="rounded-2xl bg-emerald-950/95 border border-emerald-600 p-4">
            <div class="flex items-start gap-3">
              <span class="text-3xl leading-none">✓</span>
              <div class="min-w-0 flex-1">
                <p class="text-base font-bold text-emerald-300">Ticked off</p>
                <p class="font-mono text-sm font-semibold truncate mt-0.5">{pickCard.part?.mpn ?? '—'}</p>
                <p class="text-xs text-emerald-200/80 mt-0.5">
                  {pickDone} of {pickTotal} found{pickCard.quantity != null ? ` · need ${pickCard.quantity.toLocaleString()}` : ''}{pickCard.note ? ` · ${pickCard.note}` : ''}
                </p>
              </div>
              {#if lastPickedItemId}
                <button
                  onclick={undoLastPick}
                  class="shrink-0 rounded-lg border border-emerald-700 px-2.5 py-1.5 text-xs font-semibold text-emerald-200 active:bg-emerald-900"
                >
                  Undo
                </button>
              {/if}
            </div>
          </div>
        {:else if pickCard.kind === 'already'}
          <div class="rounded-2xl bg-sky-950/95 border border-sky-600 p-4">
            <div class="flex items-start gap-3">
              <span class="text-3xl leading-none">↺</span>
              <div class="min-w-0 flex-1">
                <p class="text-base font-bold text-sky-300">Already ticked</p>
                <p class="font-mono text-sm font-semibold truncate mt-0.5">{pickCard.part?.mpn ?? '—'}</p>
                <p class="text-xs text-sky-200/80 mt-0.5">A duplicate bag — it goes on the same heap.</p>
              </div>
            </div>
          </div>
        {:else if pickCard.kind === 'not_on_list'}
          <div class="rounded-2xl bg-red-950/95 border border-red-700 p-4">
            <div class="flex items-start gap-3">
              <span class="text-3xl leading-none">✗</span>
              <div class="min-w-0 flex-1">
                <p class="text-base font-bold text-red-300">Not on {selected.name}</p>
                <p class="font-mono text-sm font-semibold truncate mt-0.5">{pickCard.part.mpn ?? '—'}</p>
                {#if pickCard.part.description}
                  <p class="text-xs text-red-200/70 truncate mt-0.5">{pickCard.part.description}</p>
                {/if}
              </div>
            </div>
          </div>
        {:else if pickCard.kind === 'unknown'}
          <div class="rounded-2xl bg-zinc-800/95 border border-zinc-600 p-4">
            <div class="flex items-center gap-3">
              <span class="text-3xl leading-none">?</span>
              <div class="min-w-0 flex-1">
                <p class="text-sm font-semibold text-zinc-300">Unknown barcode — can't tell</p>
                <p class="font-mono text-xs text-zinc-500 truncate">{pickCard.label}</p>
              </div>
            </div>
          </div>
        {:else}
          <div class="rounded-2xl bg-zinc-800/95 border border-zinc-600 p-4">
            <p class="text-sm font-semibold text-zinc-300">{pickCard.message}</p>
          </div>
        {/if}
      </div>
      {/key}
    {/if}

    <!-- Outstanding sheet: what is still to find in the pile. Tappable, because
         cut tape and house-labelled bins carry no barcode. -->
    {#if sheetOpen}
      <div class="absolute inset-0 z-40 flex flex-col bg-zinc-950">
        <div class="flex items-center justify-between gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
          <div class="min-w-0">
            <p class="font-medium truncate">Still to find</p>
            <p class="text-xs text-zinc-400">{outstanding.length} of {pickTotal} outstanding</p>
          </div>
          <button
            onclick={() => (sheetOpen = false)}
            class="shrink-0 rounded-lg bg-white px-3 py-1.5 text-sm font-semibold text-black active:bg-zinc-200"
          >
            Back to scanning
          </button>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto p-4 space-y-1.5">
          {#each detailItems as it (it.id)}
            {@const loc = itemLocationLabel(it)}
            <button
              onclick={() => tickItem(it, it.checked_at === null)}
              disabled={busy}
              class="w-full rounded-lg px-3 py-2.5 text-left disabled:opacity-50
                {it.checked_at ? 'bg-emerald-950/60 border border-emerald-800/60' : 'bg-zinc-900'}"
            >
              <div class="flex items-baseline justify-between gap-3">
                <p class="min-w-0 truncate font-mono text-sm font-medium">
                  {#if it.checked_at}<span class="mr-1 text-emerald-400">✓</span>{/if}{it.mpn ?? it.description ?? '—'}
                </p>
                <span class="shrink-0 text-xs tabular-nums {it.quantity == null ? 'text-amber-400' : 'text-zinc-300'}">
                  {it.quantity != null ? `× ${it.quantity.toLocaleString()}` : 'no qty'}
                </span>
              </div>
              {#if loc}
                <p class="mt-1 truncate text-[11px] text-zinc-400">📍 {loc}</p>
              {/if}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Resume prompt: continuing a half-done run is the point, but a stale
         selection would make the progress count a lie. -->
    {#if showStartFresh}
      <div class="absolute inset-0 z-50 flex items-center justify-center bg-black/80 px-6">
        <div class="w-full max-w-sm rounded-2xl bg-zinc-900 p-6 space-y-4">
          <h2 class="text-base font-semibold">{pickDone} row{pickDone === 1 ? '' : 's'} already ticked</h2>
          <p class="text-sm text-zinc-400">
            Continue where you left off, or clear the ticks and start this pile from scratch?
          </p>
          <div class="flex gap-3">
            <button
              onclick={() => startFresh(true)}
              class="flex-1 rounded-lg border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
            >
              Start fresh
            </button>
            <button
              onclick={() => startFresh(false)}
              class="flex-1 rounded-lg bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
            >
              Continue
            </button>
          </div>
        </div>
      </div>
    {/if}
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
      {#key scanPulse.seq}
      <div class="scan-card absolute inset-x-0 bottom-32 z-20 px-3">
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
      {/key}
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

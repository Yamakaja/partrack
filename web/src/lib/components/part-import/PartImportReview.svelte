<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import {
    markBulkScanItemReviewed,
    markBulkScanItemDismissed,
    retryBulkScanItem,
    retryBulkScanItemDistributors,
    type BulkScanItem,
    type BulkScanTaskType,
    type EnrichmentDataAggregate,
  } from '$lib/api/bulkScan.js';
  import { commitResolveItem } from '$lib/api/partResolve.js';
  import { getPart, listParts } from '$lib/api/parts.js';
  import { createInventoryEntry, createAdjustment } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import { listDistributorConfigs } from '$lib/api/distributors.js';
  import { ExternalLink, Star } from 'lucide-svelte';
  import StorageLocationPicker, { type StorageLocationValue } from '$lib/components/storage/StorageLocationPicker.svelte';
  import AdjustStockForm from '$lib/components/inventory/AdjustStockForm.svelte';

  let {
    items,
    teamId,
    sessionId,
    onItemActioned,
  }: {
    items: BulkScanItem[];
    teamId: string;
    sessionId: string;
    onItemActioned?: (itemId: string, action: 'reviewed' | 'dismissed') => void;
  } = $props();

  // ── Item grouping ─────────────────────────────────────────────────────────

  function isReady(item: BulkScanItem): boolean {
    if (item.status !== 'pending_review') return false;
    if (!item.resolve_item) return true;
    return item.resolve_item.status === 'enriched' || item.resolve_item.status === 'failed';
  }

  const readyItems = $derived(items.filter(isReady));
  const processingItems = $derived(
    items.filter((i) => i.status === 'pending_review' && i.resolve_item?.status === 'pending'),
  );
  const doneItems = $derived(
    items.filter((i) => i.status === 'reviewed' || i.status === 'dismissed'),
  );

  // ── Active item selection ─────────────────────────────────────────────────

  let activeItemId = $state<string | null>(null);
  let submitting = $state(false);
  let submitError = $state('');

  const activeItem = $derived(items.find((i) => i.id === activeItemId) ?? null);

  // effective task type: scan item's own (synchronous) or resolved via resolve_item
  const activeTaskType = $derived<BulkScanTaskType | null>(
    activeItem?.task_type ?? activeItem?.resolve_item?.task_type ?? null,
  );

  const activeRI = $derived(activeItem?.resolve_item ?? null);
  const activeEnrichment = $derived<EnrichmentDataAggregate | null>(
    activeRI?.enrichment_data ?? null,
  );

  // Auto-select first ready item when list changes
  $effect(() => {
    if (activeItemId && readyItems.some((i) => i.id === activeItemId)) return;
    activeItemId = readyItems[0]?.id ?? null;
  });

  $effect(() => {
    if (activeItemId) submitError = '';
  });

  // ── Remote data ───────────────────────────────────────────────────────────

  const distributorConfigsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
    staleTime: 60_000,
  }));

  const activePartQuery = createQuery(() => ({
    queryKey: ['part', activeItem?.part_id, teamId],
    queryFn: () => getPart(activeItem!.part_id!, teamId),
    enabled: !!(
      activeItem?.part_id &&
      (activeTaskType === 'known_part_stock_adjustment' ||
        activeTaskType === 'known_part_create_entry')
    ),
  }));

  const locationsQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
    staleTime: 60_000,
  }));

  const locations = $derived(locationsQuery.data?.items ?? []);
  const activePart = $derived(activePartQuery.data ?? null);
  const preferredDistId = $derived(
    distributorConfigsQuery.data?.items.find((c) => c.preferred)?.distributor_id ?? null,
  );

  // ── Unknown part import state ─────────────────────────────────────────────

  const dists = $derived(activeEnrichment?.distributors ?? []);

  // Key format: first 36 chars = distributor UUID, char 37 = ':', rest = SKU.
  // UUIDs never contain ':' so this is unambiguous regardless of SKU content.
  function listingKey(distId: string, sku: string): string {
    return `${distId}:${sku}`;
  }
  function parseListingKey(key: string): { distId: string; sku: string } {
    return { distId: key.slice(0, 36), sku: key.slice(37) };
  }

  function findInitialPrimaryListingKey(): string {
    const withListings = dists.filter((d) =>
      (d.results ?? []).some((r) => (r.listings?.length ?? 0) > 0),
    );
    if (withListings.length === 0) return '';
    const preferred =
      withListings.find((d) => d.distributor_id === preferredDistId) ?? withListings[0];
    const firstResult = preferred.results?.find((r) => (r.listings?.length ?? 0) > 0);
    if (!firstResult) return '';
    return listingKey(preferred.distributor_id, firstResult.listings![0].sku);
  }

  let primaryListingKey = $state('');
  let includedListings = $state<Set<string>>(new Set());

  let importInitialQty = $state('');
  let importInitialReason = $state('initial_count');
  let importStorageLoc = $state<StorageLocationValue | null>(null);

  // ── Known part create entry state ─────────────────────────────────────────

  let createQty = $state('');
  let createStorageLoc = $state<StorageLocationValue | null>(null);

  // ── Known part adjust state ───────────────────────────────────────────────

  let adjustMode = $state<'delta' | 'set'>('delta');
  let adjustQty = $state('');
  let adjustReason = $state('received');
  let lastUsedReason = $state('received');

  // ── Unrecognized state ────────────────────────────────────────────────────

  let searchQuery = $state('');
  let searchResults = $state<Array<{
    id: string; mpn: string | null; manufacturer: string | null; description: string | null;
  }>>([]);
  let searching = $state(false);
  let selectedSearchPartId = $state<string | null>(null);
  let searchTimer: ReturnType<typeof setTimeout> | null = null;

  const selectedPartForSearchQuery = createQuery(() => ({
    queryKey: ['part', selectedSearchPartId, teamId],
    queryFn: () => getPart(selectedSearchPartId!, teamId),
    enabled: !!selectedSearchPartId,
  }));
  const selectedSearchPart = $derived(selectedPartForSearchQuery.data ?? null);
  const searchPartHasInventory = $derived(!!selectedSearchPart?.inventory_entry_id);

  let unrecCreateQty = $state('');
  let unrecCreateStorageLoc = $state<StorageLocationValue | null>(null);
  let unrecAdjustMode = $state<'delta' | 'set'>('delta');
  let unrecAdjustQty = $state('');
  let unrecAdjustReason = $state('received');

  // ── Reset state on active item change ────────────────────────────────────

  $effect(() => {
    const item = activeItem;
    if (!item) return;
    submitError = '';
    primaryListingKey = '';
    includedListings = new Set();
    importInitialQty = item.initial_qty != null ? String(item.initial_qty) : '';
    importInitialReason = 'initial_count';
    importStorageLoc = null;
    createQty = item.initial_qty != null ? String(item.initial_qty) : '';
    createStorageLoc = null;
    adjustMode = 'delta';
    adjustQty = item.initial_qty != null ? String(item.initial_qty) : '';
    adjustReason = lastUsedReason;
    searchQuery = '';
    searchResults = [];
    selectedSearchPartId = null;
    unrecCreateQty = '';
    unrecCreateStorageLoc = null;
    unrecAdjustMode = 'delta';
    unrecAdjustQty = '';
    unrecAdjustReason = lastUsedReason;
  });

  // Initialise listing selection once enrichment data is available
  $effect(() => {
    if (!activeEnrichment || primaryListingKey !== '') return;
    const pk = findInitialPrimaryListingKey();
    primaryListingKey = pk;
    const included = new Set<string>();
    for (const d of activeEnrichment.distributors) {
      for (const result of d.results ?? []) {
        for (const listing of result.listings ?? []) {
          const key = listingKey(d.distributor_id, listing.sku);
          if (key !== pk) included.add(key);
        }
      }
    }
    includedListings = included;
  });

  function setPrimaryListing(key: string) {
    const s = new Set(includedListings);
    if (primaryListingKey) s.add(primaryListingKey);
    s.delete(key);
    primaryListingKey = key;
    includedListings = s;
  }

  function toggleListing(key: string) {
    const s = new Set(includedListings);
    if (s.has(key)) s.delete(key); else s.add(key);
    includedListings = s;
  }

  // ── Search (unrecognized) ─────────────────────────────────────────────────

  function onSearchInput() {
    if (searchTimer) clearTimeout(searchTimer);
    selectedSearchPartId = null;
    if (!searchQuery.trim()) { searchResults = []; return; }
    searchTimer = setTimeout(async () => {
      searching = true;
      try {
        const res = await listParts({ q: searchQuery, team_id: teamId, limit: 10 });
        searchResults = res.items.map((p) => ({
          id: p.id, mpn: p.mpn, manufacturer: p.manufacturer, description: p.description,
        }));
      } finally {
        searching = false;
      }
    }, 300);
  }

  // ── Navigation ────────────────────────────────────────────────────────────

  function selectNext(excludeId: string) {
    activeItemId = readyItems.find((i) => i.id !== excludeId)?.id ?? null;
  }

  function navigateUp() {
    const idx = readyItems.findIndex((i) => i.id === activeItemId);
    if (idx > 0) activeItemId = readyItems[idx - 1].id;
  }

  function navigateDown() {
    const idx = readyItems.findIndex((i) => i.id === activeItemId);
    if (idx >= 0 && idx < readyItems.length - 1) activeItemId = readyItems[idx + 1].id;
  }

  // ── Accept ────────────────────────────────────────────────────────────────

  async function doAccept() {
    if (!activeItem) return;
    const tt = activeTaskType;
    const ri = activeRI;

    if (tt === 'unknown_part_import') {
      if (!ri) throw new Error('No resolve item');
      const { distId: primaryDistId, sku: primarySku } = parseListingKey(primaryListingKey);
      const body: Parameters<typeof commitResolveItem>[2] = {
        primary_distributor_id: primaryDistId,
        primary_sku: primarySku,
        additional_listings: [...includedListings].map((key) => {
          const { distId, sku } = parseListingKey(key);
          return { distributor_id: distId, sku };
        }),
      };
      const importQtyNum = parseInt(importInitialQty, 10);
      if (!isNaN(importQtyNum) && importQtyNum !== 0) {
        body.initial_qty = importQtyNum;
        body.initial_qty_reason = importInitialReason;
      }
      if (importStorageLoc?.locationId) body.storage_location_id = importStorageLoc.locationId;
      const { part_id } = await commitResolveItem(teamId, ri.id, body);
      await markBulkScanItemReviewed(teamId, sessionId, activeItem.id, part_id);

    } else if (tt === 'known_part_create_entry') {
      const pid = activeItem.part_id;
      if (!pid) throw new Error('No part linked');
      const entry = await createInventoryEntry(teamId, {
        part_id: pid,
        storage_location_id: createStorageLoc?.locationId ?? undefined,
        storage_coordinate: createStorageLoc?.coordinate ?? undefined,
      });
      const qty = parseInt(createQty, 10);
      if (!isNaN(qty) && qty > 0) {
        await createAdjustment(teamId, entry.id, { delta: qty, reason: 'initial_stock' });
      }
      await markBulkScanItemReviewed(teamId, sessionId, activeItem.id, pid);

    } else if (tt === 'known_part_stock_adjustment') {
      if (!activePart) throw new Error('Part not loaded');
      if (!activePart.inventory_entry_id) throw new Error('No inventory entry found');
      const n = parseInt(adjustQty, 10);
      if (isNaN(n)) throw new Error('Enter a valid quantity');
      const delta = adjustMode === 'delta' ? n : n - (activePart.quantity_on_hand ?? 0);
      if (delta === 0) throw new Error('No change');
      lastUsedReason = adjustReason;
      await createAdjustment(teamId, activePart.inventory_entry_id, { delta, reason: adjustReason });
      await markBulkScanItemReviewed(teamId, sessionId, activeItem.id, activeItem.part_id ?? undefined);

    } else if (tt === 'unrecognized') {
      if (!selectedSearchPartId) throw new Error('Select a part first');
      const part = selectedSearchPart;
      if (!part) throw new Error('Part not loaded');
      if (searchPartHasInventory) {
        if (!part.inventory_entry_id) throw new Error('No inventory entry found');
        const n = parseInt(unrecAdjustQty, 10);
        if (isNaN(n)) throw new Error('Enter a valid quantity');
        const delta = unrecAdjustMode === 'delta' ? n : n - (part.quantity_on_hand ?? 0);
        if (delta !== 0) {
          lastUsedReason = unrecAdjustReason;
          await createAdjustment(teamId, part.inventory_entry_id, { delta, reason: unrecAdjustReason });
        }
      } else {
        const entry = await createInventoryEntry(teamId, {
          part_id: selectedSearchPartId,
          storage_location_id: unrecCreateStorageLoc?.locationId ?? undefined,
          storage_coordinate: unrecCreateStorageLoc?.coordinate ?? undefined,
        });
        const qty = parseInt(unrecCreateQty, 10);
        if (!isNaN(qty) && qty > 0) {
          await createAdjustment(teamId, entry.id, { delta: qty, reason: 'initial_stock' });
        }
      }
      await markBulkScanItemReviewed(teamId, sessionId, activeItem.id, selectedSearchPartId);
    }

    onItemActioned?.(activeItem.id, 'reviewed');
    selectNext(activeItem.id);
  }

  async function doAcceptWrapped() {
    submitting = true;
    submitError = '';
    try {
      await doAccept();
    } catch (e: unknown) {
      submitError = e instanceof Error ? e.message : 'Action failed';
    } finally {
      submitting = false;
    }
  }

  async function doDismiss() {
    if (!activeItem) return;
    submitting = true;
    submitError = '';
    try {
      await markBulkScanItemDismissed(teamId, sessionId, activeItem.id);
      onItemActioned?.(activeItem.id, 'dismissed');
      selectNext(activeItem.id);
    } catch (e: unknown) {
      submitError = e instanceof Error ? e.message : 'Dismiss failed';
    } finally {
      submitting = false;
    }
  }

  async function handleRetryItem(itemId: string) {
    try {
      await retryBulkScanItem(teamId, sessionId, itemId);
      onItemActioned?.(itemId, 'reviewed');
    } catch {}
  }

  async function handleRetryDistributor(distributorId: string) {
    if (!activeItem) return;
    try {
      await retryBulkScanItemDistributors(teamId, sessionId, activeItem.id, [distributorId]);
      onItemActioned?.(activeItem.id, 'reviewed');
    } catch {}
  }

  // ── Keyboard shortcuts ────────────────────────────────────────────────────

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowUp') { e.preventDefault(); navigateUp(); return; }
    if (e.key === 'ArrowDown') { e.preventDefault(); navigateDown(); return; }
    const inInput = e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement || e.target instanceof HTMLTextAreaElement;
    if (inInput && e.key !== 'Enter') return;
    if (e.key === 'Enter' && !submitting && activeItem && isReady(activeItem)) {
      e.preventDefault(); doAcceptWrapped();
    }
    if (e.key === 'Escape' && !submitting && activeItem) {
      e.preventDefault(); doDismiss();
    }
  }

  // ── Helpers ───────────────────────────────────────────────────────────────

  function itemLabel(item: BulkScanItem): string {
    if (item.part?.mpn) return item.part.mpn;
    if (item.parsed_fields?.mpn) return item.parsed_fields.mpn;
    if (item.parsed_fields?.sku) return item.parsed_fields.sku;
    return item.barcode.slice(0, 24);
  }

  function taskLabel(tt: BulkScanTaskType | null): string {
    switch (tt) {
      case 'unknown_part_import': return 'Import Part';
      case 'known_part_create_entry': return 'Create Inventory';
      case 'known_part_stock_adjustment': return 'Stock Adjustment';
      case 'unrecognized': return 'Unrecognized';
      default: return '';
    }
  }

  function formatPrice(price: string | undefined): string {
    if (!price) return '';
    const n = parseFloat(price);
    return isNaN(n) ? price : `$${n.toFixed(4)}`;
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="flex flex-1 overflow-hidden">

  <!-- ── Left panel: item list ──────────────────────────────────────────── -->
  <div class="w-72 shrink-0 border-r border-border flex flex-col overflow-hidden">
    <div class="flex-1 overflow-y-auto">

      {#if processingItems.length > 0}
        <div class="px-3 pt-4 pb-1">
          <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Processing ({processingItems.length})</h3>
        </div>
        {#each processingItems as item (item.id)}
          <div class="mx-2 mb-1 rounded-md px-3 py-2 flex items-center gap-2 text-muted-foreground opacity-60">
            <div class="h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
            <span class="text-xs truncate">{itemLabel(item)}</span>
          </div>
        {/each}
      {/if}

      {#if readyItems.length > 0}
        <div class="px-3 pt-4 pb-1">
          <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Ready ({readyItems.length})</h3>
        </div>
        {#each readyItems as item (item.id)}
          {@const tt = item.task_type ?? item.resolve_item?.task_type ?? null}
          {@const failed = !tt && item.resolve_item?.status === 'failed'}
          <div class="mx-2 mb-1">
            <button
              onclick={() => { activeItemId = item.id; }}
              class="w-full rounded-md px-3 py-2 text-left transition-colors
                {activeItemId === item.id ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50'}"
            >
              <p class="text-xs font-medium truncate {failed ? 'text-destructive' : ''}">{itemLabel(item)}</p>
              {#if tt}
                <p class="text-xs text-muted-foreground">{taskLabel(tt)}</p>
              {:else if failed}
                <p class="text-xs text-destructive/70">Enrichment failed</p>
              {/if}
            </button>
            {#if failed}
              <button
                onclick={() => handleRetryItem(item.id)}
                class="mt-0.5 w-full rounded-md border border-border px-3 py-1 text-left text-xs text-muted-foreground hover:bg-accent/50"
              >Retry</button>
            {/if}
          </div>
        {/each}
      {/if}

      {#if doneItems.length > 0}
        <div class="px-3 pt-4 pb-1">
          <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Done ({doneItems.length})</h3>
        </div>
        {#each doneItems as item (item.id)}
          <div class="mx-2 mb-1 rounded-md px-3 py-2 flex items-center gap-2 opacity-50">
            <span class="text-xs {item.status === 'dismissed' ? 'text-muted-foreground' : 'text-green-600 dark:text-green-400'}">
              {item.status === 'reviewed' ? '✓' : '×'}
            </span>
            <span class="text-xs truncate">{itemLabel(item)}</span>
          </div>
        {/each}
      {/if}

      {#if processingItems.length === 0 && readyItems.length === 0 && doneItems.length > 0}
        <div class="p-4 pt-6 text-center">
          <p class="text-sm font-medium">All done!</p>
          <p class="mt-1 text-xs text-muted-foreground">All items reviewed or dismissed.</p>
        </div>
      {:else if processingItems.length === 0 && readyItems.length === 0 && doneItems.length === 0}
        <div class="p-6 text-center">
          <p class="text-sm text-muted-foreground">No items.</p>
        </div>
      {/if}
    </div>
  </div>

  <!-- ── Right panel: task UI ───────────────────────────────────────────── -->
  <div class="flex flex-1 flex-col overflow-hidden">
    {#if !activeItem}
      <div class="flex flex-1 items-center justify-center">
        <p class="text-sm text-muted-foreground">
          {readyItems.length > 0 ? 'Select an item to review.' : 'Nothing to review.'}
        </p>
      </div>
    {:else}
      <!-- Content zone -->
      <div class="flex-1 overflow-y-auto p-6 space-y-6">

        {#if activeTaskType === null && activeRI?.status === 'pending'}
          <div class="flex items-center gap-3 text-muted-foreground">
            <div class="h-5 w-5 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
            <p class="text-sm">Enriching part data…</p>
          </div>

        {:else if activeTaskType === null && activeRI?.status === 'failed'}
          <div>
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Enrichment Failed</h3>
            <div class="rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm">
              <p class="text-destructive">{activeRI.error_message ?? 'All distributors failed to return results.'}</p>
            </div>
            <button
              onclick={() => handleRetryItem(activeItem.id)}
              class="mt-4 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent/50"
            >Retry</button>
          </div>

        {:else if activeTaskType === 'unknown_part_import' && activeEnrichment}
          <!-- ── Unknown part import ──────────────────────────────────── -->
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Import Part</h3>

          <!-- Part info header — use the product that contains the primary listing -->
          {@const primaryDistId = primaryListingKey.slice(0, 36)}
          {@const primarySku = primaryListingKey.slice(37)}
          {@const primaryDist = dists.find((d) => d.distributor_id === primaryDistId)}
          {@const partInfo =
            primaryDist?.results?.find((r) => r.listings?.some((l) => l.sku === primarySku)) ??
            primaryDist?.results?.[0] ??
            dists.find((d) => d.results?.length)?.results?.[0] ??
            null}
          {#if partInfo}
            <div class="rounded-lg border border-border bg-card p-4 space-y-1.5 text-sm">
              {#if partInfo.mpn}
                <div class="flex justify-between gap-4">
                  <span class="text-muted-foreground">MPN</span>
                  <span class="font-mono font-medium">{partInfo.mpn}</span>
                </div>
              {/if}
              {#if partInfo.manufacturer}
                <div class="flex justify-between gap-4">
                  <span class="text-muted-foreground">Manufacturer</span>
                  <span>{partInfo.manufacturer}</span>
                </div>
              {/if}
              {#if partInfo.description}
                <p class="text-xs text-muted-foreground pt-0.5">{partInfo.description}</p>
              {/if}
              {#if partInfo.datasheet_url}
                <a href={partInfo.datasheet_url} target="_blank" rel="noopener noreferrer"
                  class="text-xs text-primary hover:underline">Datasheet ↗</a>
              {/if}
            </div>
          {/if}

          <!-- Distributor sections -->
          <div class="space-y-3">
            {#each dists as dist (dist.distributor_id)}
              {@const distHasPrimary =
                (dist.results ?? []).some((r) =>
                  r.listings?.some((l) => listingKey(dist.distributor_id, l.sku) === primaryListingKey),
                )}
              {@const hasListings = (dist.results ?? []).some((r) => (r.listings?.length ?? 0) > 0)}
              <div class="rounded-lg border overflow-hidden {distHasPrimary ? 'border-primary/60' : 'border-border'}">
                <!-- Header -->
                <div class="flex items-center gap-3 px-4 py-2.5 bg-muted/40 border-b border-border">
                  <span class="text-sm font-medium flex-1 truncate">{dist.name}</span>
                  {#if dist.error}
                    <button
                      type="button"
                      onclick={() => handleRetryDistributor(dist.distributor_id)}
                      class="shrink-0 rounded border border-border px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent/50"
                    >Retry</button>
                  {/if}
                </div>

                {#if dist.error}
                  <p class="px-4 py-2.5 text-xs text-destructive/80">{dist.error.split('\n')[0]}</p>
                {:else if hasListings}
                  <div class="divide-y divide-border">
                    {#each (dist.results ?? []) as result (result.mpn ?? result)}
                      {#if (result.listings?.length ?? 0) > 0}
                        <!-- Product header -->
                        <div class="px-4 py-2 bg-muted/20 border-b border-border/60">
                          <div class="flex items-center gap-2 flex-wrap">
                            {#if result.mpn}
                              <span class="font-mono text-xs font-semibold">{result.mpn}</span>
                            {/if}
                            {#if result.manufacturer}
                              <span class="text-xs text-muted-foreground">{result.manufacturer}</span>
                            {/if}
                          </div>
                          {#if result.description}
                            <p class="text-xs text-muted-foreground mt-0.5 line-clamp-2">{result.description}</p>
                          {/if}
                        </div>
                        {#each result.listings! as listing}
                          {@const lKey = listingKey(dist.distributor_id, listing.sku)}
                          {@const isListingPrimary = lKey === primaryListingKey}
                          {@const isIncluded = isListingPrimary || includedListings.has(lKey)}
                          <div class="px-3 py-2 flex items-center gap-2.5 text-xs
                            {isListingPrimary ? 'bg-accent/20' : ''}
                            {!isIncluded ? 'opacity-40' : ''}">
                            <!-- Include checkbox (disabled for primary — always included) -->
                            <input
                              type="checkbox"
                              checked={isIncluded}
                              disabled={isListingPrimary}
                              onchange={() => toggleListing(lKey)}
                              class="h-3.5 w-3.5 rounded shrink-0"
                            />
                            <!-- SKU -->
                            <span class="font-mono flex-1 min-w-0 truncate">{listing.sku}</span>
                            <!-- Packaging -->
                            {#if listing.packaging_type}
                              <span class="text-muted-foreground shrink-0">{listing.packaging_type}</span>
                            {/if}
                            <!-- Price -->
                            {#if listing.unit_price}
                              <span class="tabular-nums font-medium shrink-0">{formatPrice(listing.unit_price)}/ea</span>
                            {/if}
                            <!-- Stock badge -->
                            {#if listing.stock_available === null || listing.stock_available === undefined}
                              <span class="inline-flex items-center rounded-full border border-border px-2 py-0.5 font-semibold shrink-0">?</span>
                            {:else if listing.stock_available === 0}
                              <span class="inline-flex items-center rounded-full border-transparent bg-destructive text-destructive-foreground px-2 py-0.5 font-semibold shrink-0">0</span>
                            {:else}
                              <span class="inline-flex items-center rounded-full border-transparent bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 px-2 py-0.5 font-semibold shrink-0">{listing.stock_available.toLocaleString()}</span>
                            {/if}
                            <!-- MOQ -->
                            <span class="text-muted-foreground shrink-0">MOQ {listing.moq}</span>
                            <!-- Inline action buttons (ListingTable style) -->
                            <div class="flex items-center gap-0.5 shrink-0 ml-auto">
                              {#if listing.listing_url}
                                <a
                                  href={listing.listing_url}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  class="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                                  title="Open product page"
                                >
                                  <ExternalLink class="h-3.5 w-3.5" />
                                </a>
                              {/if}
                              <button
                                type="button"
                                onclick={() => setPrimaryListing(lKey)}
                                title={isListingPrimary ? 'Primary listing (used for enrichment)' : 'Set as primary listing'}
                                class="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                              >
                                <Star class="h-3.5 w-3.5 {isListingPrimary ? 'fill-yellow-400 text-yellow-400' : ''}" />
                              </button>
                            </div>
                          </div>
                        {/each}
                      {/if}
                    {/each}
                  </div>
                {:else if !dist.error}
                  <p class="px-4 py-2.5 text-xs text-muted-foreground">No listings found.</p>
                {/if}
              </div>
            {/each}
          </div>

          <!-- Qty / reason / storage -->
          <div class="grid grid-cols-2 gap-4 pt-2 border-t border-border">
            <div>
              <label for="import-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
              <input id="import-qty" type="number" min="0" bind:value={importInitialQty} placeholder="0"
                class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
            </div>
            {#if parseInt(importInitialQty, 10) > 0}
              <div>
                <label for="import-reason" class="text-xs font-medium text-muted-foreground block mb-1">Reason</label>
                <select id="import-reason" bind:value={importInitialReason}
                  class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring">
                  <option value="initial_count">Initial count</option>
                  <option value="received">Received</option>
                  <option value="correction">Correction</option>
                </select>
              </div>
            {:else}
              <div>
                <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
                <StorageLocationPicker value={importStorageLoc} {locations} onSelect={(v) => { importStorageLoc = v; }} />
              </div>
            {/if}
          </div>
          {#if parseInt(importInitialQty, 10) > 0}
            <div>
              <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
              <StorageLocationPicker value={importStorageLoc} {locations} onSelect={(v) => { importStorageLoc = v; }} />
            </div>
          {/if}

        {:else if activeTaskType === 'known_part_create_entry'}
          <!-- ── Known part — create inventory entry ────────────────── -->
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Create Inventory Entry</h3>
          {#if activePartQuery.isPending}
            <div class="flex items-center gap-2 text-sm text-muted-foreground">
              <div class="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
              Loading…
            </div>
          {:else if activePart}
            <div class="rounded-lg border border-border bg-card p-4 mb-4 space-y-2 text-sm">
              {#if activePart.mpn}
                <div class="flex justify-between"><span class="text-muted-foreground">MPN</span><span class="font-mono font-medium">{activePart.mpn}</span></div>
              {/if}
              {#if activePart.manufacturer}
                <div class="flex justify-between"><span class="text-muted-foreground">Manufacturer</span><span>{activePart.manufacturer}</span></div>
              {/if}
              {#if activePart.description}
                <p class="text-xs text-muted-foreground">{activePart.description}</p>
              {/if}
            </div>
          {/if}
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label for="create-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
              <input id="create-qty" type="number" min="0" bind:value={createQty} placeholder="0"
                class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
            </div>
            <div>
              <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
              <StorageLocationPicker value={createStorageLoc} {locations} onSelect={(v) => { createStorageLoc = v; }} />
            </div>
          </div>

        {:else if activeTaskType === 'known_part_stock_adjustment'}
          <!-- ── Known part — stock adjustment ─────────────────────── -->
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Stock Adjustment</h3>
          {#if activePartQuery.isPending}
            <div class="flex items-center gap-2 text-sm text-muted-foreground">
              <div class="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
              Loading…
            </div>
          {:else if activePart}
            <div class="max-w-[800px] rounded-lg border border-border bg-card p-4 mb-4 text-sm">
              <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2">
                {#if activePart.mpn}
                  <dt class="text-muted-foreground">MPN</dt><dd class="font-mono font-medium">{activePart.mpn}</dd>
                {/if}
                {#if activePart.manufacturer}
                  <dt class="text-muted-foreground">Manufacturer</dt><dd>{activePart.manufacturer}</dd>
                {/if}
                <dt class="text-muted-foreground">Current stock</dt>
                <dd class="font-medium tabular-nums">{activePart.quantity_on_hand ?? 0}</dd>
              </dl>
            </div>
          {/if}
          <div class="max-w-[480px]">
            <AdjustStockForm
              currentQuantity={activePart?.quantity_on_hand ?? undefined}
              bind:mode={adjustMode}
              bind:value={adjustQty}
              bind:reason={adjustReason}
            />
          </div>

        {:else if activeTaskType === 'unrecognized'}
          <!-- ── Unrecognized barcode ────────────────────────────────── -->
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Unrecognized Barcode</h3>
          <div class="rounded-lg border border-border bg-card p-4 mb-4">
            <p class="text-xs font-medium text-muted-foreground mb-1">Raw barcode</p>
            <p class="font-mono text-sm break-all">{activeItem.barcode}</p>
          </div>
          <div class="mb-4">
            <label for="barcode-search" class="text-xs font-medium text-muted-foreground block mb-1">Search parts</label>
            <input id="barcode-search" type="text" bind:value={searchQuery} oninput={onSearchInput}
              placeholder="Search by MPN, description…"
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
          </div>
          {#if searching}
            <p class="text-xs text-muted-foreground">Searching…</p>
          {:else if searchResults.length > 0}
            <div class="space-y-1 mb-4">
              {#each searchResults as part (part.id)}
                <button
                  onclick={() => { selectedSearchPartId = part.id; }}
                  class="w-full rounded-lg border p-3 text-left text-sm transition-colors
                    {selectedSearchPartId === part.id ? 'border-primary bg-primary/5' : 'border-border hover:bg-accent/50'}"
                >
                  <p class="font-mono text-xs font-medium">{part.mpn ?? '—'}</p>
                  <p class="text-xs text-muted-foreground">
                    {part.manufacturer ?? ''}{part.description ? ` · ${part.description}` : ''}
                  </p>
                </button>
              {/each}
            </div>
          {/if}

          {#if selectedSearchPartId && selectedSearchPart}
            <div class="border-t border-border pt-4">
              {#if searchPartHasInventory}
                <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-3">Stock Adjustment</p>
                <div class="rounded-lg border border-border bg-card p-3 mb-3 text-sm">
                  <div class="flex justify-between">
                    <span class="text-muted-foreground">Current stock</span>
                    <span class="font-medium tabular-nums">{selectedSearchPart.quantity_on_hand ?? 0}</span>
                  </div>
                </div>
                <div class="max-w-[480px]">
                  <AdjustStockForm
                    currentQuantity={selectedSearchPart.quantity_on_hand ?? undefined}
                    bind:mode={unrecAdjustMode}
                    bind:value={unrecAdjustQty}
                    bind:reason={unrecAdjustReason}
                  />
                </div>
              {:else}
                <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-3">Create Inventory Entry</p>
                <div class="grid grid-cols-2 gap-4">
                  <div>
                    <label for="unrec-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
                    <input id="unrec-qty" type="number" min="0" bind:value={unrecCreateQty} placeholder="0"
                      class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
                  </div>
                  <div>
                    <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
                    <StorageLocationPicker value={unrecCreateStorageLoc} {locations} onSelect={(v) => { unrecCreateStorageLoc = v; }} />
                  </div>
                </div>
              {/if}
            </div>
          {/if}

        {:else}
          <div class="flex items-center gap-3 text-muted-foreground">
            <div class="h-5 w-5 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
            <p class="text-sm">Loading…</p>
          </div>
        {/if}

        {#if submitError}
          <p class="text-sm text-destructive">{submitError}</p>
        {/if}
      </div>

      <!-- Fixed action bar -->
      {#if activeTaskType !== null || (activeRI?.status !== 'pending' && activeRI?.status !== 'failed')}
        <div class="border-t border-border px-6 py-4 flex items-center justify-between gap-4 bg-background">
          <button
            onclick={doDismiss}
            disabled={submitting}
            class="rounded-md border border-border px-4 py-2 text-sm font-medium hover:bg-accent/50 disabled:opacity-50"
          >Dismiss (Esc)</button>
          <button
            onclick={doAcceptWrapped}
            disabled={submitting || (activeTaskType === 'unrecognized' && !selectedSearchPartId)}
            class="rounded-md bg-primary px-6 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >{submitting ? 'Working…' : 'Accept (Enter)'}</button>
        </div>
      {/if}
    {/if}
  </div>

</div>

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    listBulkScanItems,
    markBulkScanItemReviewed,
    markBulkScanItemDismissed,
    retryBulkScanItem,
    retryBulkScanItemDistributors,
    type BulkScanItem,
  } from '$lib/api/bulkScan.js';
  import { importPart, addListing, getPart, listParts } from '$lib/api/parts.js';
  import { updateInventoryEntry, createInventoryEntry, createAdjustment } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { linkAction } from '$lib/router/index.svelte.js';
  import AdjustStockForm from '$lib/components/inventory/AdjustStockForm.svelte';
  import StorageLocationPicker, { type StorageLocationValue } from '$lib/components/storage/StorageLocationPicker.svelte';

  let { teamId, sessionId }: { teamId: string; sessionId: string } = $props();

  const qc = useQueryClient();

  const itemsQuery = createQuery(() => ({
    queryKey: ['bulk-scan-items', teamId, sessionId],
    queryFn: () => listBulkScanItems(teamId, sessionId),
    refetchInterval: 5000,
  }));

  const locationsQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  // ── Active item selection ─────────────────────────────────────────────────

  let activeItemId = $state<string | null>(null);
  let submitting = $state(false);
  let submitError = $state('');

  // Session-level last-used reason for stock adjustments
  let lastUsedReason = $state('received');

  const items = $derived(itemsQuery.data?.items ?? []);
  const pending = $derived(items.filter(i => i.status === 'pending_enrichment'));
  const ready = $derived(items.filter(i => i.status === 'enriched'));
  const errors = $derived(items.filter(i => i.status === 'error'));

  const activeItem = $derived(items.find(i => i.id === activeItemId) ?? null);

  // Auto-select first ready item when list loads or changes
  $effect(() => {
    if (activeItemId && items.find(i => i.id === activeItemId && (i.status === 'enriched' || i.status === 'error'))) return;
    const first = ready[0] ?? errors[0];
    activeItemId = first?.id ?? null;
  });

  // ── Task state ────────────────────────────────────────────────────────────

  // unknown_part_import
  interface SelectedImportListing { distIdx: number; listingIdx: number; }
  let selectedImportListings = $state<SelectedImportListing[]>([]);
  let importQty = $state('');
  let importStorageLoc = $state<StorageLocationValue | null>(null);

  function isListingSelected(distIdx: number, listingIdx: number): boolean {
    return selectedImportListings.some(l => l.distIdx === distIdx && l.listingIdx === listingIdx);
  }
  function toggleListing(distIdx: number, listingIdx: number) {
    if (isListingSelected(distIdx, listingIdx)) {
      selectedImportListings = selectedImportListings.filter(l => !(l.distIdx === distIdx && l.listingIdx === listingIdx));
    } else {
      selectedImportListings = [...selectedImportListings, { distIdx, listingIdx }];
    }
  }

  // known_part_create_entry
  let createQty = $state('');
  let createStorageLoc = $state<StorageLocationValue | null>(null);

  // known_part_stock_adjustment
  type AdjustMode = 'delta' | 'set';
  let adjustMode = $state<AdjustMode>('delta');
  let adjustQty = $state('');
  let adjustReason = $state('');

  // unrecognized_barcode
  let searchQuery = $state('');
  let searchResults = $state<Array<{ id: string; mpn: string | null; manufacturer: string | null; description: string | null }>>([]);
  let searching = $state(false);
  let selectedSearchPartId = $state<string | null>(null);
  let selectedSearchPartName = $state('');

  // Reset task state when active item changes
  $effect(() => {
    const item = activeItem;
    if (!item) return;
    submitError = '';

    // Default: select all available listings across all distributors.
    const dists = item.enrichment_data?.distributors ?? [];
    const allListings: SelectedImportListing[] = [];
    dists.forEach((d, di) => {
      d.result?.listings.forEach((_, li) => allListings.push({ distIdx: di, listingIdx: li }));
    });
    selectedImportListings = allListings;

    importQty = item.initial_qty != null ? String(item.initial_qty) : '';
    importStorageLoc = null;
    createQty = item.initial_qty != null ? String(item.initial_qty) : '';
    createStorageLoc = null;
    adjustMode = 'delta';
    adjustQty = item.initial_qty != null ? String(item.initial_qty) : '';
    adjustReason = lastUsedReason;
    searchQuery = '';
    searchResults = [];
    selectedSearchPartId = null;
    selectedSearchPartName = '';
  });

  // ── Search for parts (unrecognized_barcode) ───────────────────────────────

  let searchTimer: ReturnType<typeof setTimeout> | null = null;

  function onSearchInput() {
    if (searchTimer) clearTimeout(searchTimer);
    if (!searchQuery.trim()) { searchResults = []; return; }
    searchTimer = setTimeout(async () => {
      searching = true;
      try {
        const res = await listParts({ q: searchQuery, team_id: teamId, limit: 10 });
        searchResults = res.items.map(p => ({
          id: p.id,
          mpn: p.mpn,
          manufacturer: p.manufacturer,
          description: p.description,
        }));
      } finally {
        searching = false;
      }
    }, 300);
  }

  // ── Active part data (for stock on hand) ─────────────────────────────────

  const activePartQuery = createQuery(() => ({
    queryKey: ['part', activeItem?.part_id, teamId],
    queryFn: () => getPart(activeItem!.part_id!, teamId),
    enabled: !!(activeItem?.part_id && (activeItem.task_type === 'known_part_stock_adjustment' || activeItem.task_type === 'known_part_create_entry')),
  }));

  // ── All distributor results (including errors) ────────────────────────────

  const allEnrichmentDists = $derived(
    activeItem?.enrichment_data?.distributors ?? []
  );

  // ── Actions ───────────────────────────────────────────────────────────────

  function selectNextReady(currentId: string) {
    const actionable = [...ready, ...errors].filter(i => i.id !== currentId);
    activeItemId = actionable[0]?.id ?? null;
  }

  async function handleAccept() {
    if (!activeItem) return;
    submitting = true;
    submitError = '';
    try {
      await doAccept(activeItem);
      await markBulkScanItemReviewed(teamId, sessionId, activeItem.id);
      qc.invalidateQueries({ queryKey: ['bulk-scan-items', teamId, sessionId] });
      qc.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
      selectNextReady(activeItem.id);
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Action failed';
      submitError = msg;
    } finally {
      submitting = false;
    }
  }

  async function doAccept(item: BulkScanItem) {
    if (item.task_type === 'unknown_part_import') {
      const toImport = selectedImportListings
        .map(l => ({ dist: allEnrichmentDists[l.distIdx], listing: allEnrichmentDists[l.distIdx]?.result?.listings[l.listingIdx] }))
        .filter((l): l is { dist: typeof allEnrichmentDists[0]; listing: NonNullable<typeof allEnrichmentDists[0]['result']>['listings'][0] } => !!(l.dist && l.listing && l.listing.sku));
      if (toImport.length === 0) throw new Error('Select at least one listing with a valid SKU');
      const { part_id } = await importPart(teamId, {
        sku: toImport[0].listing.sku,
        distributor_id: toImport[0].dist.distributor_id,
      });
      for (const { dist, listing } of toImport.slice(1)) {
        await addListing(part_id, teamId, { sku: listing.sku, distributor_id: dist.distributor_id });
      }
      // importPart already creates the inventory entry — fetch it rather than
      // creating a duplicate (which would 409 on the unique (team_id, part_id) constraint).
      const imported = await getPart(part_id, teamId);
      if (!imported.inventory_entry_id) throw new Error('No inventory entry after import');
      if (importStorageLoc) {
        await updateInventoryEntry(teamId, imported.inventory_entry_id, {
          storage_location_id: importStorageLoc.locationId,
          storage_coordinate: importStorageLoc.coordinate ?? undefined,
        });
      }
      const qty = parseInt(importQty, 10);
      if (!isNaN(qty) && qty > 0) {
        await createAdjustment(teamId, imported.inventory_entry_id, { delta: qty, reason: 'initial_stock' });
      }

    } else if (item.task_type === 'known_part_create_entry') {
      if (!item.part_id) throw new Error('No part linked');
      const entry = await createInventoryEntry(teamId, {
        part_id: item.part_id,
        storage_location_id: createStorageLoc?.locationId || undefined,
        storage_coordinate: createStorageLoc?.coordinate ?? undefined,
      });
      const qty = parseInt(createQty, 10);
      if (!isNaN(qty) && qty > 0) {
        await createAdjustment(teamId, entry.id, { delta: qty, reason: 'initial_stock' });
      }

    } else if (item.task_type === 'known_part_stock_adjustment') {
      if (!item.part_id) throw new Error('No part linked');
      const part = await getPart(item.part_id, teamId);
      if (!part.inventory_entry_id) throw new Error('No inventory entry found');
      const n = parseInt(adjustQty, 10);
      if (isNaN(n)) throw new Error('Enter a valid quantity');
      const delta = adjustMode === 'delta' ? n : n - (part.quantity_on_hand ?? 0);
      if (delta === 0) throw new Error('No change');
      lastUsedReason = adjustReason;
      await createAdjustment(teamId, part.inventory_entry_id, { delta, reason: adjustReason });

    } else if (item.task_type === 'unrecognized_barcode') {
      if (!selectedSearchPartId) throw new Error('Select a part first');
      // Mark reviewed — user identified the part; no automatic inventory action
    }
  }

  async function handleCancel() {
    if (!activeItem) return;
    submitting = true;
    try {
      await markBulkScanItemDismissed(teamId, sessionId, activeItem.id);
      qc.invalidateQueries({ queryKey: ['bulk-scan-items', teamId, sessionId] });
      qc.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
      selectNextReady(activeItem.id);
    } finally {
      submitting = false;
    }
  }

  async function handleRetry(itemId: string) {
    await retryBulkScanItem(teamId, sessionId, itemId);
    qc.invalidateQueries({ queryKey: ['bulk-scan-items', teamId, sessionId] });
    qc.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
  }

  async function handleRetryDistributor(distributorId: string) {
    if (!activeItem) return;
    await retryBulkScanItemDistributors(teamId, sessionId, activeItem.id, [distributorId]);
    qc.invalidateQueries({ queryKey: ['bulk-scan-items', teamId, sessionId] });
  }

  // ── Keyboard shortcuts ────────────────────────────────────────────────────

  function onKeydown(e: KeyboardEvent) {
    const inInput = e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement || e.target instanceof HTMLTextAreaElement;
    if (inInput && e.key !== 'Enter') return;
    if (e.key === 'Enter' && !submitting && activeItem) { e.preventDefault(); handleAccept(); }
    if (e.key === 'Escape' && !submitting && activeItem) { e.preventDefault(); handleCancel(); }
  }

  const locations = $derived(locationsQuery.data?.items ?? []);

  function formatPrice(price: string | undefined): string {
    if (!price) return '';
    const n = parseFloat(price);
    return isNaN(n) ? price : `$${n.toFixed(4)}`;
  }

  function taskLabel(type: string | null): string {
    switch (type) {
      case 'unknown_part_import': return 'Import Part';
      case 'known_part_create_entry': return 'Create Inventory';
      case 'known_part_stock_adjustment': return 'Stock Adjustment';
      case 'unrecognized_barcode': return 'Unrecognized';
      default: return 'Unknown';
    }
  }

  function itemSummary(item: BulkScanItem): string {
    if (item.part) return item.part.mpn ?? item.part.description ?? 'Part';
    if (item.parsed_fields?.mpn) return item.parsed_fields.mpn;
    if (item.parsed_fields?.sku) return item.parsed_fields.sku;
    return item.barcode.slice(0, 24);
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="flex flex-col h-full">
  <Topbar title="Review">
    {#snippet actions()}
      <a
        href={`/teams/${teamId}/bulk-scan`}
        use:linkAction
        class="text-sm text-muted-foreground hover:text-foreground"
      >← All sessions</a>
    {/snippet}
  </Topbar>

  {#if itemsQuery.isPending}
    <div class="flex flex-1 items-center justify-center">
      <p class="text-sm text-muted-foreground">Loading…</p>
    </div>
  {:else if itemsQuery.isError}
    <div class="flex flex-1 items-center justify-center">
      <p class="text-sm text-destructive">Failed to load items.</p>
    </div>
  {:else}
    <div class="flex flex-1 overflow-hidden">

      <!-- ── Left panel: item list ─────────────────────────────────────────── -->
      <div class="w-72 shrink-0 border-r border-border flex flex-col overflow-hidden">
        <div class="flex-1 overflow-y-auto">

          {#if pending.length > 0}
            <div class="px-3 pt-4 pb-1">
              <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Pending ({pending.length})</h3>
            </div>
            {#each pending as item (item.id)}
              <div class="mx-2 mb-1 rounded-md px-3 py-2 flex items-center gap-2 text-muted-foreground opacity-60">
                <div class="h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
                <span class="text-xs truncate">{itemSummary(item)}</span>
              </div>
            {/each}
          {/if}

          {#if ready.length > 0}
            <div class="px-3 pt-4 pb-1">
              <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Ready ({ready.length})</h3>
            </div>
            {#each ready as item (item.id)}
              <button
                onclick={() => { activeItemId = item.id; }}
                class="w-full mx-2 mb-1 rounded-md px-3 py-2 text-left transition-colors
                  {activeItemId === item.id ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50'}"
                style="width: calc(100% - 1rem)"
              >
                <p class="text-xs font-medium truncate">{itemSummary(item)}</p>
                <p class="text-xs text-muted-foreground">{taskLabel(item.task_type)}</p>
              </button>
            {/each}
          {/if}

          {#if errors.length > 0}
            <div class="px-3 pt-4 pb-1">
              <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Errors ({errors.length})</h3>
            </div>
            {#each errors as item (item.id)}
              <div class="mx-2 mb-1">
                <button
                  onclick={() => { activeItemId = item.id; }}
                  class="w-full rounded-md px-3 py-2 text-left transition-colors
                    {activeItemId === item.id ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50'}"
                >
                  <p class="text-xs font-medium truncate text-destructive">{itemSummary(item)}</p>
                  <p class="text-xs text-muted-foreground truncate">{item.error_message ?? 'Error'}</p>
                </button>
                <button
                  onclick={() => handleRetry(item.id)}
                  class="mt-0.5 w-full rounded-md border border-border px-3 py-1 text-left text-xs text-muted-foreground hover:bg-accent/50"
                >
                  Retry
                </button>
              </div>
            {/each}
          {/if}

          {#if pending.length === 0 && ready.length === 0 && errors.length === 0}
            <div class="p-6 text-center">
              <p class="text-sm font-medium">All done!</p>
              <p class="mt-1 text-xs text-muted-foreground">All items have been reviewed or dismissed.</p>
              <a
                href={`/teams/${teamId}/bulk-scan`}
                use:linkAction
                class="mt-3 inline-block text-xs text-primary hover:underline"
              >← Back to sessions</a>
            </div>
          {/if}
        </div>
      </div>

      <!-- ── Right panel: task UI ──────────────────────────────────────────── -->
      <div class="flex flex-1 flex-col overflow-hidden">
        {#if !activeItem}
          <div class="flex flex-1 items-center justify-center">
            <p class="text-sm text-muted-foreground">Select an item to review.</p>
          </div>
        {:else}
          <!-- Content zone -->
          <div class="flex-1 overflow-y-auto p-6 space-y-6">
            {#if activeItem.task_type === 'unknown_part_import'}
              <!-- ── Unknown part import ──────────────────────────────────── -->
              <div class="space-y-6">
                <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Import Part</h3>

                {#if allEnrichmentDists.length === 0}
                  <p class="text-sm text-muted-foreground">No distributor results available.</p>
                {:else}
                  <!-- One section per distributor -->
                  {#each allEnrichmentDists as dist, di}
                    <div class="rounded-lg border border-border overflow-hidden">
                      <!-- Distributor header -->
                      <div class="flex items-center justify-between bg-muted/40 px-4 py-2.5 border-b border-border">
                        <span class="text-sm font-medium">{dist.name}</span>
                        {#if dist.error}
                          <div class="flex items-center gap-2">
                            <span class="text-xs text-destructive/80 truncate max-w-[240px]" title={dist.error}>Failed: {dist.error.split('\n')[0]}</span>
                            <button
                              type="button"
                              onclick={() => handleRetryDistributor(dist.distributor_id)}
                              class="shrink-0 rounded border border-border px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent/50 transition-colors"
                            >Retry</button>
                          </div>
                        {:else if dist.result}
                          <span class="text-xs text-muted-foreground">
                            {dist.result.mpn ?? ''}{dist.result.manufacturer ? ` · ${dist.result.manufacturer}` : ''}
                          </span>
                        {/if}
                      </div>

                      {#if dist.result?.listings.length}
                        <div class="divide-y divide-border">
                          {#each dist.result.listings as listing, li}
                            {@const checked = isListingSelected(di, li)}
                            {@const noSku = !listing.sku}
                            <button
                              type="button"
                              disabled={noSku}
                              onclick={() => toggleListing(di, li)}
                              class="w-full px-4 py-3 text-left transition-colors disabled:opacity-50 disabled:cursor-default
                                {!noSku && checked ? 'bg-primary/5' : !noSku ? 'hover:bg-accent/40' : ''}"
                            >
                              <div class="flex items-center gap-3">
                                <span class="flex h-4 w-4 shrink-0 items-center justify-center rounded border
                                  {noSku ? 'border-muted-foreground/40' : checked ? 'border-primary bg-primary text-primary-foreground' : 'border-muted-foreground'}">
                                  {#if checked && !noSku}
                                    <svg class="h-2.5 w-2.5" fill="none" stroke="currentColor" viewBox="0 0 12 12">
                                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2 6l3 3 5-5"/>
                                    </svg>
                                  {/if}
                                </span>
                                <span class="font-mono text-xs flex-1 {noSku ? 'text-muted-foreground italic' : ''}">{noSku ? 'No SKU' : listing.sku}</span>
                                <span class="text-xs text-muted-foreground">{listing.packaging_type}</span>
                                <span class="text-xs font-medium tabular-nums">{formatPrice(listing.unit_price)}/ea</span>
                              </div>
                              <div class="mt-1 ml-7 flex gap-4 text-xs text-muted-foreground">
                                {#if listing.stock_available != null}
                                  <span>{listing.stock_available.toLocaleString()} in stock</span>
                                {/if}
                                <span>MOQ {listing.moq}</span>
                              </div>
                            </button>
                          {/each}
                        </div>
                      {:else if !dist.error}
                        <p class="px-4 py-3 text-sm text-muted-foreground">No listings found.</p>
                      {/if}
                    </div>
                  {/each}
                {/if}

                <!-- Quantity + storage -->
                <div class="grid grid-cols-2 gap-4 pt-2 border-t border-border">
                  <div>
                    <label for="import-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
                    <input
                      id="import-qty"
                      type="number" min="0"
                      bind:value={importQty}
                      placeholder="0"
                      class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </div>
                  <div>
                    <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
                    <StorageLocationPicker
                      value={importStorageLoc}
                      locations={locations}
                      onSelect={(v) => { importStorageLoc = v; }}
                    />
                  </div>
                </div>
              </div>

            {:else if activeItem.task_type === 'known_part_create_entry'}
              <!-- ── Known part — create inventory entry ─────────────────── -->
              <div>
                <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Create Inventory Entry</h3>
                {#if activeItem.part}
                  <div class="rounded-lg border border-border bg-card p-4 mb-4 space-y-2 text-sm">
                    {#if activeItem.part.mpn}
                      <div class="flex justify-between">
                        <span class="text-muted-foreground">MPN</span>
                        <span class="font-mono font-medium">{activeItem.part.mpn}</span>
                      </div>
                    {/if}
                    {#if activeItem.part.manufacturer}
                      <div class="flex justify-between">
                        <span class="text-muted-foreground">Manufacturer</span>
                        <span>{activeItem.part.manufacturer}</span>
                      </div>
                    {/if}
                    {#if activeItem.part.description}
                      <p class="text-muted-foreground text-xs">{activeItem.part.description}</p>
                    {/if}
                  </div>
                {/if}
                <div class="grid grid-cols-2 gap-4">
                  <div>
                    <label for="create-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
                    <input
                      id="create-qty"
                      type="number" min="0"
                      bind:value={createQty}
                      placeholder="0"
                      class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </div>
                  <div>
                    <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
                    <StorageLocationPicker
                      value={createStorageLoc}
                      locations={locations}
                      onSelect={(v) => { createStorageLoc = v; }}
                    />
                  </div>
                </div>
              </div>

            {:else if activeItem.task_type === 'known_part_stock_adjustment'}
              <!-- ── Known part — stock adjustment ───────────────────────── -->
              <div>
                <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Stock Adjustment</h3>
                {#if activeItem.part}
                  <div class="max-w-[800px] rounded-lg border border-border bg-card p-4 mb-4 text-sm">
                    <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2">
                      {#if activeItem.part.mpn}
                        <dt class="text-muted-foreground">MPN</dt>
                        <dd class="font-mono font-medium">{activeItem.part.mpn}</dd>
                      {/if}
                      {#if activeItem.part.manufacturer}
                        <dt class="text-muted-foreground">Manufacturer</dt>
                        <dd>{activeItem.part.manufacturer}</dd>
                      {/if}
                      <dt class="text-muted-foreground">Current stock</dt>
                      <dd class="font-medium tabular-nums">
                        {#if activePartQuery.isPending}…{:else}{activePartQuery.data?.quantity_on_hand ?? 0}{/if}
                      </dd>
                    </dl>
                  </div>
                {/if}
                <div class="max-w-[800px]">
                  <AdjustStockForm
                    currentQuantity={activePartQuery.data?.quantity_on_hand ?? undefined}
                    bind:mode={adjustMode}
                    bind:value={adjustQty}
                    bind:reason={adjustReason}
                  />
                </div>
              </div>

            {:else if activeItem.task_type === 'unrecognized_barcode'}
              <!-- ── Unrecognized barcode ─────────────────────────────────── -->
              <div>
                <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Unrecognized Barcode</h3>
                <div class="rounded-lg border border-border bg-card p-4 mb-4">
                  <p class="text-xs font-medium text-muted-foreground mb-1">Raw barcode</p>
                  <p class="font-mono text-sm break-all">{activeItem.barcode}</p>
                </div>
                <div class="mb-4">
                  <label for="barcode-search" class="text-xs font-medium text-muted-foreground block mb-1">Search parts</label>
                  <input
                    id="barcode-search"
                    type="text"
                    bind:value={searchQuery}
                    oninput={onSearchInput}
                    placeholder="Search by MPN, description…"
                    class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
                {#if searching}
                  <p class="text-xs text-muted-foreground">Searching…</p>
                {:else if searchResults.length > 0}
                  <div class="space-y-1">
                    {#each searchResults as part}
                      <button
                        onclick={() => { selectedSearchPartId = part.id; selectedSearchPartName = part.mpn ?? part.description ?? part.id; }}
                        class="w-full rounded-lg border p-3 text-left text-sm transition-colors
                          {selectedSearchPartId === part.id ? 'border-primary bg-primary/5' : 'border-border hover:bg-accent/50'}"
                      >
                        <p class="font-medium font-mono text-xs">{part.mpn ?? '—'}</p>
                        <p class="text-xs text-muted-foreground">{part.manufacturer ?? ''} {part.description ? `· ${part.description}` : ''}</p>
                      </button>
                    {/each}
                  </div>
                {/if}
                {#if selectedSearchPartId}
                  <p class="mt-2 text-xs text-muted-foreground">Selected: <span class="font-medium">{selectedSearchPartName}</span></p>
                {/if}
              </div>
            {/if}

            {#if submitError}
              <p class="text-sm text-destructive">{submitError}</p>
            {/if}
          </div>

          <!-- Fixed action bar -->
          <div class="border-t border-border px-6 py-4 flex items-center justify-between gap-4 bg-background">
            <button
              onclick={handleCancel}
              disabled={submitting}
              class="rounded-md border border-border px-4 py-2 text-sm font-medium hover:bg-accent/50 disabled:opacity-50"
            >
              Cancel (Esc)
            </button>
            <button
              onclick={handleAccept}
              disabled={submitting || (activeItem.task_type === 'unrecognized_barcode' && !selectedSearchPartId)}
              class="rounded-md bg-primary px-6 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {submitting ? 'Working…' : 'Accept (Enter)'}
            </button>
          </div>
        {/if}
      </div>

    </div>
  {/if}
</div>

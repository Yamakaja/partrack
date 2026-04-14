<script lang="ts">
  import { onMount } from 'svelte';
  import DistributorSearch from './DistributorSearch.svelte';
  import type { ScanPart, ScanImportContext, SelectedListing } from './types.js';
  import { importScanPartMulti, ScanExpiredError, type DistributorSearchItem } from '$lib/api/scanner.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';

  let {
    token,
    context,
    onBack,
    onImportDone,
    onExpired,
  }: {
    token: string;
    context: ScanImportContext;
    onBack: () => void;
    onImportDone: (part: ScanPart, bagQty: number) => void;
    onExpired: () => void;
  } = $props();

  // ── Internal state ────────────────────────────────────────────────────────

  type InnerScreen = 'overview' | 'searching';
  let innerScreen = $state<InnerScreen>('overview');
  let selectedListings = $state<SelectedListing[]>([]);
  let activeDistributorId = $state('');
  let activeDistributorName = $state('');
  let importing = $state(false);
  let importError = $state('');

  // ── Derived: initial checked SKUs for the active distributor ──────────────

  let activeInitialChecked = $derived(
    selectedListings
      .filter(l => l.distributor_id === activeDistributorId)
      .map(l => l.sku),
  );

  // ── Visited distributor tracking (sequential auto-search) ─────────────────

  let visitedDistributors = $state<Set<string>>(new Set());

  // ── Initialization ────────────────────────────────────────────────────────

  onMount(() => {
    // Auto-search the bag's own distributor first when we have enough info to
    // query it (SKU, MPN, or manufacturer). Fall back to preferred distributor
    // when the bag has no usable distributor info.
    const hasBagDistInfo = context.parsedDistributorId &&
      (context.parsedSKU || context.parsedMPN || context.parsedManufacturer);

    if (hasBagDistInfo) {
      openSearch(context.parsedDistributorId, context.parsedDistributorName);
    } else if (context.preferredDistributorId) {
      openSearch(context.preferredDistributorId, context.preferredDistributorName);
    }
    // No distributor info at all → show empty overview; user picks manually.
  });

  // ── Search navigation ─────────────────────────────────────────────────────

  function openSearch(distId: string, distName: string) {
    activeDistributorId = distId;
    activeDistributorName = distName;
    innerScreen = 'searching';
  }

  function backFromSearch() {
    visitedDistributors = new Set([...visitedDistributors, activeDistributorId]);
    innerScreen = 'overview';
    maybeAutoNavigateNext();
  }

  // After returning from any search, auto-navigate to the preferred distributor
  // if it hasn't been visited yet and differs from the bag's distributor.
  function maybeAutoNavigateNext() {
    if (
      context.preferredDistributorId &&
      context.preferredDistributorId !== context.parsedDistributorId &&
      !visitedDistributors.has(context.preferredDistributorId)
    ) {
      openSearch(context.preferredDistributorId, context.preferredDistributorName);
    }
  }

  function searchQueryFor(distId: string): string {
    if (distId === context.parsedDistributorId && context.parsedSKU) {
      return context.parsedSKU;
    }
    const parts = [context.parsedManufacturer, context.parsedMPN].filter(Boolean);
    return parts.length > 0 ? parts.join(' ') : context.parsedSKU;
  }

  // ── Listing management ────────────────────────────────────────────────────

  function handleSelectListings(items: DistributorSearchItem[]) {
    visitedDistributors = new Set([...visitedDistributors, activeDistributorId]);
    // Replace semantics: remove all existing listings for this distributor,
    // then set to the newly confirmed selection.
    const others = selectedListings.filter(l => l.distributor_id !== activeDistributorId);
    const newListings: SelectedListing[] = items.map(item => ({
      distributor_id: activeDistributorId,
      distributor_name: activeDistributorName,
      sku: item.primary_sku,
      mpn: item.mpn || undefined,
      manufacturer: item.manufacturer || undefined,
      description: item.description || undefined,
      packaging: item.package || undefined,
      unit_price: item.unit_price || undefined,
      stock_available: item.stock_available,
    }));
    selectedListings = [...others, ...newListings];
    innerScreen = 'overview';
    maybeAutoNavigateNext();
  }

  function removeListing(index: number) {
    selectedListings = selectedListings.filter((_, i) => i !== index);
  }

  // ── Import ────────────────────────────────────────────────────────────────

  async function confirmImport() {
    importing = true;
    importError = '';
    try {
      const data = await importScanPartMulti(token, {
        listings: selectedListings.map(l => ({ distributor_id: l.distributor_id, sku: l.sku })),
      });

      if (data.failed.length > 0) {
        const names = data.failed.map(f => `${f.sku} (${f.reason})`).join(', ');
        pushToast('warning', `${data.failed.length} listing${data.failed.length === 1 ? '' : 's'} could not be added`, names);
      }

      const first = selectedListings[0];
      const part: ScanPart = {
        id: data.part_id,
        mpn: first?.mpn ?? null,
        description: first?.description ?? null,
        manufacturer: first?.manufacturer ?? null,
        inventory_entry_id: null,
        quantity_on_hand: null,
        storage_location: null,
        sku: first?.sku ?? null,
        distributor_name: first?.distributor_name ?? null,
      };
      onImportDone(part, context.parsedQty ?? 0);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      importError = 'Import failed. Try again.';
    } finally {
      importing = false;
    }
  }
</script>

{#if innerScreen === 'searching'}
  <!-- ── DISTRIBUTOR SEARCH (multi-select sub-screen) ───────────────────── -->
  <!-- {#key} forces a remount when the active distributor changes, even when  -->
  <!-- state updates are batched and innerScreen stays 'searching'.            -->
  {#key activeDistributorId}
    <DistributorSearch
      {token}
      mode="multi"
      distributorId={activeDistributorId}
      distributorName={activeDistributorName}
      initialQuery={searchQueryFor(activeDistributorId)}
      initialCheckedSkus={activeInitialChecked}
      onBack={backFromSearch}
      onSelectListings={handleSelectListings}
      {onExpired}
    />
  {/key}

{:else}
  <!-- ── OVERVIEW ──────────────────────────────────────────────────────────── -->
  <div class="flex flex-1 flex-col min-h-0">
    <!-- Header -->
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
      <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Import Part</p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <!-- Part info card -->
      {#if context.parsedMPN || context.parsedManufacturer || context.parsedQty != null}
        <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
          {#if context.parsedMPN}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">MPN</span>
              <span class="font-medium font-mono text-xs">{context.parsedMPN}</span>
            </div>
          {/if}
          {#if context.parsedManufacturer}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Manufacturer</span>
              <span class="font-medium text-xs">{context.parsedManufacturer}</span>
            </div>
          {/if}
          {#if context.parsedQty != null}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Qty</span>
              <span class="font-medium">{context.parsedQty}</span>
            </div>
          {/if}
        </div>
      {/if}

      <!-- Selected listings -->
      <div>
        <p class="text-xs font-semibold text-zinc-500 uppercase tracking-wide mb-2">Selected listings</p>
        {#if selectedListings.length === 0}
          <p class="text-sm text-zinc-600 py-3 text-center">No listings selected</p>
        {:else}
          <div class="rounded-xl bg-zinc-900 divide-y divide-zinc-800">
            {#each selectedListings as listing, idx}
              <div class="flex items-start gap-3 px-4 py-3">
                <div class="flex-1 min-w-0">
                  <div class="flex items-baseline justify-between gap-2">
                    <span class="text-sm font-medium">{listing.distributor_name}</span>
                    <span class="text-xs font-mono text-zinc-400 shrink-0">{listing.sku}</span>
                  </div>
                  {#if listing.packaging || listing.unit_price}
                    <p class="text-xs text-zinc-500 mt-0.5">
                      {#if listing.packaging}{listing.packaging}{/if}{#if listing.packaging && listing.unit_price} · {/if}{#if listing.unit_price}<PriceDisplay value={listing.unit_price} decimals={4} />{/if}
                    </p>
                  {/if}
                </div>
                <button
                  onclick={() => removeListing(idx)}
                  aria-label="Remove listing"
                  class="text-zinc-600 active:text-white shrink-0 mt-0.5"
                >
                  <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      <!-- Add from distributor buttons -->
      {#if context.distributorConfigs.length > 0}
        <div class="flex flex-col gap-2">
          {#each context.distributorConfigs as cfg}
            <button
              onclick={() => openSearch(cfg.id, cfg.name)}
              class="w-full rounded-lg border border-zinc-700 py-3 text-base font-semibold text-zinc-300 active:bg-zinc-800"
            >
              Add from {cfg.name}
            </button>
          {/each}
        </div>
      {/if}

      {#if importError}
        <p class="text-xs text-red-400 text-center">{importError}</p>
      {/if}
    </div>

    <!-- Import CTA -->
    <div class="shrink-0 border-t border-zinc-800 p-4">
      <button
        onclick={confirmImport}
        disabled={selectedListings.length === 0 || importing}
        class="relative w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200 disabled:opacity-40"
      >
        {#if importing}
          <span class="opacity-0">Import Part</span>
          <div class="absolute inset-0 flex items-center justify-center">
            <div class="h-5 w-5 animate-spin rounded-full border-2 border-zinc-400 border-t-black"></div>
          </div>
        {:else}
          Import Part
        {/if}
      </button>
    </div>
  </div>
{/if}

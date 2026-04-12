<script lang="ts">
  import { onMount } from 'svelte';
  import type { ScanPart } from './types.js';
  import { searchDistributor, importScanPart, ScanExpiredError, type DistributorSearchItem } from '$lib/api/scanner.js';

  let {
    token,
    distributorId,
    distributorName,
    initialQuery,
    parsedQty,
    onBack,
    onImportDone,
    onExpired,
  }: {
    token: string;
    distributorId: string;
    distributorName: string;
    initialQuery: string;
    parsedQty: number | null;
    onBack: () => void;
    onImportDone: (part: ScanPart, bagQty: number) => void;
    onExpired: () => void;
  } = $props();

  // ── State ─────────────────────────────────────────────────────────────────

  let results = $state<DistributorSearchItem[]>([]);
  let searchLoading = $state(true);
  let searchError = $state('');
  let importingIdx = $state<number | null>(null);
  let importError = $state('');

  // ── Search on mount ───────────────────────────────────────────────────────

  onMount(async () => {
    try {
      const data = await searchDistributor(token, distributorId, initialQuery);
      results = data.items ?? [];
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      searchError = 'Search failed. Go back and try again.';
    } finally {
      searchLoading = false;
    }
  });

  // ── Import on select ──────────────────────────────────────────────────────

  async function selectResult(idx: number, item: DistributorSearchItem) {
    importingIdx = idx;
    importError = '';
    try {
      const data = await importScanPart(token, { distributor_id: distributorId, sku: item.primary_sku });
      const part: ScanPart = {
        id: data.part_id,
        mpn: item.mpn || null,
        description: item.description || null,
        inventory_entry_id: null,
        quantity_on_hand: null,
        storage_location: null,
        manufacturer: item.manufacturer || null,
        sku: item.primary_sku,
        distributor_name: distributorName,
      };
      onImportDone(part, parsedQty ?? 0);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      importError = 'Import failed. Try another result or go back.';
    } finally {
      importingIdx = null;
    }
  }
</script>

<div class="flex flex-1 flex-col min-h-0">
  <!-- Header -->
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <div class="min-w-0">
      <p class="font-medium">Search {distributorName}</p>
      <p class="text-xs text-zinc-500 truncate">{initialQuery}</p>
    </div>
  </div>

  <!-- Loading -->
  {#if searchLoading}
    <div class="flex flex-1 flex-col items-center justify-center gap-4 px-8 text-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
      <p class="text-sm text-zinc-400">Searching {distributorName}…</p>
    </div>

  <!-- Error -->
  {:else if searchError}
    <div class="flex flex-1 flex-col items-center justify-center gap-4 px-8 text-center">
      <p class="text-sm text-zinc-400">{searchError}</p>
      <button onclick={onBack} class="rounded-lg border border-zinc-700 px-6 py-2 text-sm text-zinc-300 active:bg-zinc-800">
        Go Back
      </button>
    </div>

  <!-- Results -->
  {:else}
    <div class="flex-1 overflow-y-auto min-h-0">
      {#if importError}
        <p class="px-4 pt-3 text-xs text-red-400 text-center">{importError}</p>
      {/if}

      {#if results.length === 0}
        <div class="flex flex-1 flex-col items-center justify-center gap-2 py-16 px-8 text-center">
          <p class="text-sm text-zinc-400">No results for "{initialQuery}"</p>
        </div>
      {:else}
        <div class="divide-y divide-zinc-800">
          {#each results as item, idx}
            <button
              onclick={() => selectResult(idx, item)}
              disabled={importingIdx !== null}
              class="relative w-full px-4 py-3 text-left active:bg-zinc-900 disabled:opacity-60"
            >
              <!-- Importing overlay for this row -->
              {#if importingIdx === idx}
                <div class="absolute inset-0 flex items-center justify-center bg-black/60 rounded">
                  <div class="h-5 w-5 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
                </div>
              {/if}

              <div class="flex items-baseline justify-between gap-2">
                <span class="font-medium text-sm font-mono">{item.mpn}</span>
                {#if item.manufacturer}
                  <span class="text-xs text-zinc-500 shrink-0">{item.manufacturer}</span>
                {/if}
              </div>
              {#if item.description}
                <p class="text-xs text-zinc-400 mt-0.5 line-clamp-1">{item.description}</p>
              {/if}
              {#if item.unit_price || item.stock_available != null}
                <p class="text-xs text-zinc-500 mt-0.5">
                  {#if item.unit_price}${item.unit_price} each{/if}{#if item.unit_price && item.stock_available != null} · {/if}{#if item.stock_available != null}{item.stock_available.toLocaleString()} in stock{/if}
                </p>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

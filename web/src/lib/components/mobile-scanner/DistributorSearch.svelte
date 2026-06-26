<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import type { ScanPart } from './types.js';
  import { searchDistributor, importScanPart, ScanExpiredError, type DistributorSearchItem } from '$lib/api/scanner.js';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';

  let {
    distributorId,
    distributorName,
    initialQuery,
    parsedQty = null,
    mode = 'single',
    initialCheckedSkus = [],
    auditLogId = null,
    onBack,
    onImportDone,
    onSelectListings,
    onExpired,
  }: {
    distributorId: string;
    distributorName: string;
    initialQuery: string;
    parsedQty?: number | null;
    mode?: 'single' | 'multi';
    initialCheckedSkus?: string[];
    auditLogId?: string | null;
    onBack: () => void;
    onImportDone?: (part: ScanPart, bagQty: number) => void;
    onSelectListings?: (items: DistributorSearchItem[]) => void;
    onExpired: () => void;
  } = $props();

  // ── State ─────────────────────────────────────────────────────────────────

  let query = $state(untrack(() => initialQuery));
  let results = $state<DistributorSearchItem[]>([]);
  let searchLoading = $state(true);
  let searchError = $state('');
  let importingIdx = $state<number | null>(null);
  let importError = $state('');
  let searchDebounce: ReturnType<typeof setTimeout> | null = null;

  // checkedSkus drives the checkbox/count UI (reactive). selectedItems holds the
  // full item payload for every checked SKU so the confirmed selection survives a
  // re-search even when an item is no longer in the current results.
  let checkedSkus = $state<Set<string>>(new Set());
  const selectedItems = new Map<string, DistributorSearchItem>();

  // ── Search ────────────────────────────────────────────────────────────────

  function scheduleSearch() {
    if (searchDebounce) clearTimeout(searchDebounce);
    searchDebounce = setTimeout(() => runSearch(query), 300);
  }

  function searchNow() {
    if (searchDebounce) clearTimeout(searchDebounce);
    runSearch(query);
  }

  async function runSearch(q: string) {
    if (!q.trim()) {
      results = [];
      searchError = '';
      searchLoading = false;
      return;
    }
    searchLoading = true;
    searchError = '';
    try {
      const data = await searchDistributor(distributorId, q);
      results = data.items ?? [];
      // Capture full data for any (pre-)checked SKU that appears in these results so
      // a later re-search can't drop it from the confirmed selection.
      for (const item of results) {
        if (checkedSkus.has(item.primary_sku)) selectedItems.set(item.primary_sku, item);
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      results = [];
      searchError = 'Search failed. Edit your query and try again.';
    } finally {
      searchLoading = false;
    }
  }

  onMount(() => {
    if (mode === 'multi' && initialCheckedSkus.length > 0) {
      checkedSkus = new Set(initialCheckedSkus);
    }
    runSearch(query);
  });

  // ── Single mode: import on select ─────────────────────────────────────────

  async function selectResult(idx: number, item: DistributorSearchItem) {
    importingIdx = idx;
    importError = '';
    try {
      const data = await importScanPart({ distributor_id: distributorId, sku: item.primary_sku, audit_log_id: auditLogId });
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
      onImportDone?.(part, parsedQty ?? 0);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      importError = 'Import failed. Try another result or go back.';
    } finally {
      importingIdx = null;
    }
  }

  // ── Multi mode: toggle selection and confirm ──────────────────────────────

  function toggleSku(item: DistributorSearchItem) {
    const sku = item.primary_sku;
    const next = new Set(checkedSkus);
    if (next.has(sku)) {
      next.delete(sku);
      selectedItems.delete(sku);
    } else {
      next.add(sku);
      selectedItems.set(sku, item);
    }
    checkedSkus = next;
  }

  function confirmMultiSelect() {
    onSelectListings?.([...selectedItems.values()]);
  }
</script>

<div class="flex flex-1 flex-col min-h-0">
  <!-- Header with editable search -->
  <div class="border-b border-zinc-800 shrink-0">
    <div class="flex items-center gap-3 px-4 pt-3">
      <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium flex-1 truncate">Search {distributorName}</p>
      {#if searchLoading}
        <div class="h-4 w-4 animate-spin rounded-full border border-zinc-600 border-t-white shrink-0"></div>
      {/if}
    </div>
    <div class="px-4 py-2">
      <input
        type="search"
        bind:value={query}
        oninput={scheduleSearch}
        onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); searchNow(); } }}
        enterkeyhint="search"
        placeholder="Search by MPN or part number…"
        class="w-full rounded-lg bg-zinc-800 px-3 py-2 text-sm placeholder-zinc-500 outline-none focus:ring-1 focus:ring-zinc-600"
      />
    </div>
  </div>

  <!-- Body -->
  <div class="flex-1 overflow-y-auto min-h-0">
    {#if searchLoading && results.length === 0}
      <div class="flex flex-col items-center justify-center gap-4 py-16 px-8 text-center">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
        <p class="text-sm text-zinc-400">Searching {distributorName}…</p>
      </div>

    {:else if searchError}
      <div class="flex flex-col items-center justify-center gap-2 py-16 px-8 text-center">
        <p class="text-sm text-zinc-400">{searchError}</p>
      </div>

    {:else if results.length === 0}
      <div class="flex flex-col items-center justify-center gap-2 py-16 px-8 text-center">
        <p class="text-sm text-zinc-400">
          {query.trim() ? `No results for "${query}"` : 'Type to search'}
        </p>
      </div>

    {:else}
      {#if importError}
        <p class="px-4 pt-3 text-xs text-red-400 text-center">{importError}</p>
      {/if}

      <div class="divide-y divide-zinc-800">
        {#each results as item, idx}
          {#if mode === 'single'}
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
                  {#if item.unit_price}<PriceDisplay value={item.unit_price} decimals={4} /> each{/if}{#if item.unit_price && item.stock_available != null} · {/if}{#if item.stock_available != null}{item.stock_available.toLocaleString()} in stock{/if}
                </p>
              {/if}
            </button>
          {:else}
            <!-- Multi-select row -->
            {@const checked = checkedSkus.has(item.primary_sku)}
            <div
              role="checkbox"
              aria-checked={checked}
              tabindex="0"
              onclick={() => toggleSku(item)}
              onkeydown={(e) => { if (e.key === ' ' || e.key === 'Enter') { e.preventDefault(); toggleSku(item); } }}
              class="flex items-start gap-3 px-4 py-3 cursor-pointer active:bg-zinc-900 {checked ? 'bg-zinc-900' : ''}"
            >
              <div class="flex items-center mt-0.5 shrink-0">
                <input
                  type="checkbox"
                  {checked}
                  tabindex="-1"
                  class="h-4 w-4 rounded border-zinc-600 bg-zinc-800 accent-white pointer-events-none"
                  readonly
                />
              </div>
              <div class="flex-1 min-w-0">
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
                    {#if item.unit_price}<PriceDisplay value={item.unit_price} decimals={4} /> each{/if}{#if item.unit_price && item.stock_available != null} · {/if}{#if item.stock_available != null}{item.stock_available.toLocaleString()} in stock{/if}
                  </p>
                {/if}
                {#if item.package}
                  <p class="text-xs text-zinc-500 mt-0.5">{item.package}</p>
                {/if}
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}
  </div>

  <!-- Multi-select sticky footer -->
  {#if mode === 'multi'}
    <div class="shrink-0 border-t border-zinc-800 p-4">
      <button
        onclick={confirmMultiSelect}
        class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Done ({checkedSkus.size} selected)
      </button>
    </div>
  {/if}
</div>

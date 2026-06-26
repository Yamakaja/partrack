<script lang="ts">
  import { onMount } from 'svelte';
  import type { ScanPart, PartLocation } from './types.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { recordVisit } from '$lib/stores/scannerHistory.svelte.js';
  import { getPartDetail, patchInventoryEntry, scanFocus, ScanExpiredError } from '$lib/api/scanner.js';
  import { fullLocationLabel } from './locationUtils.js';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import MobileLocationPicker from './MobileLocationPicker.svelte';

  let {
    part,
    initialAdjust,
    locations,
    onBack,
    onAdjustStock,
    onExpired,
  }: {
    part: ScanPart;
    initialAdjust?: { value: number; mode: 'set' | 'adjust'; reason?: string };
    locations: StorageLocation[];
    onBack: () => void;
    onAdjustStock: (part: ScanPart, adjust?: { value?: number; mode?: 'set' | 'adjust'; reason?: string }) => void;
    onExpired: () => void;
  } = $props();

  // ── Detail data ───────────────────────────────────────────────────────────

  interface ListingItem {
    id: string;
    distributor_name: string;
    sku: string;
    packaging_type: string;
    unit_price?: string | null;
    stock_available?: number | null;
    moq: number;
  }

  interface ParamItem {
    name: string;
    value: string;
    unit?: string | null;
  }

  interface PartDetail {
    id: string;
    mpn: string | null;
    manufacturer: string | null;
    description: string | null;
    package: string | null;
    lifecycle_status: string | null;
    datasheet_url: string | null;
    product_url: string | null;
    image_url: string | null;
    category_name: string | null;
    inventory_entry_id: string | null;
    quantity_on_hand: number | null;
    quantity_reserved: number;
    quantity_available: number;
    storage_location: PartLocation | null;
    minimum_stock_quantity: number | null;
    reorder_quantity: number | null;
    listings: ListingItem[];
    parameters: ParamItem[];
  }

  let detail = $state<PartDetail | null>(null);
  let loading = $state(true);
  let showLocationPicker = $state(false);
  let assigningLocation = $state(false);

  const currentLocation = $derived(detail?.storage_location ?? part.storage_location ?? null);
  const currentLocationLabel = $derived(
    currentLocation ? fullLocationLabel(currentLocation, locations) : null,
  );

  onMount(async () => {
    try {
      detail = await getPartDetail(part.id);
      if (detail) {
        recordVisit({
          partId: part.id,
          mpn: detail.mpn,
          manufacturer: detail.manufacturer,
          description: detail.description,
          imageUrl: detail.image_url,
        });
      }
      if (initialAdjust && detail?.inventory_entry_id) {
        adjustStock();
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    } finally {
      loading = false;
    }
  });

  async function assignLocation(loc: PartLocation | null) {
    const entryId = detail?.inventory_entry_id ?? part.inventory_entry_id;
    if (!entryId) return;
    assigningLocation = true;
    try {
      await patchInventoryEntry(entryId, loc);
      if (detail) detail = { ...detail, storage_location: loc };
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    } finally {
      assigningLocation = false;
    }
  }

  // ── Helpers ───────────────────────────────────────────────────────────────

  type LifecycleStatus = 'active' | 'nrnd' | 'obsolete' | 'deprecated' | 'no_longer_available';

  function lifecyclePill(status: string | null): { bg: string; text: string; label: string } | null {
    switch (status as LifecycleStatus) {
      case 'nrnd':               return { bg: 'bg-amber-500/20',  text: 'text-amber-400',  label: 'NRND' };
      case 'obsolete':           return { bg: 'bg-red-500/20',    text: 'text-red-400',    label: 'Obsolete' };
      case 'deprecated':         return { bg: 'bg-orange-500/20', text: 'text-orange-400', label: 'Deprecated' };
      case 'no_longer_available':return { bg: 'bg-red-500/20',    text: 'text-red-400',    label: 'NLA' };
      default: return null;
    }
  }

  function adjustStock() {
    // Merge fresh detail back into ScanPart so StockAdjustment sees current data.
    const merged: ScanPart = {
      ...part,
      inventory_entry_id: detail?.inventory_entry_id ?? part.inventory_entry_id,
      quantity_on_hand:   detail?.quantity_on_hand   ?? part.quantity_on_hand,
      storage_location:   detail?.storage_location   ?? part.storage_location,
    };
    onAdjustStock(merged, initialAdjust);
  }

  const inventoryEntryId = $derived(detail?.inventory_entry_id ?? part.inventory_entry_id);
</script>

<div class="flex flex-1 flex-col min-h-0">
  <!-- Header -->
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <p class="font-medium flex-1">Part Details</p>
    <button
      onclick={() => scanFocus('part', part.id).catch(() => {})}
      class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors shrink-0"
      aria-label="Open on Desktop"
    >
      Open ↗
    </button>
  </div>

  <!-- Loading -->
  {#if loading && !detail}
    <div class="flex flex-1 items-center justify-center">
      <div class="h-6 w-6 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
    </div>

  {:else}
    <div class="flex-1 overflow-y-auto p-4 space-y-4">

      <!-- Part image -->
      {#if detail?.image_url}
        <div class="rounded-xl overflow-hidden bg-zinc-950 flex items-center justify-center" style="max-height:10rem;">
          <img src={detail.image_url} alt={detail.mpn ?? 'Part'} class="max-h-40 w-full object-contain p-4" />
        </div>
      {/if}

      <!-- Identity -->
      <div class="rounded-xl bg-zinc-900 p-4 space-y-1.5">
        <!-- MPN + lifecycle pill -->
        <div class="flex items-center gap-2 flex-wrap">
          <p class="text-lg font-semibold font-mono">{detail?.mpn ?? part.mpn ?? '—'}</p>
          {#if detail?.lifecycle_status}
            {@const pill = lifecyclePill(detail.lifecycle_status)}
            {#if pill}
              <span class="px-2 py-0.5 rounded-full text-xs font-medium {pill.bg} {pill.text}">{pill.label}</span>
            {/if}
          {/if}
        </div>

        <!-- Manufacturer + Package -->
        {#if (detail?.manufacturer ?? part.manufacturer) || detail?.package}
          <div class="flex items-baseline justify-between gap-2">
            {#if detail?.manufacturer ?? part.manufacturer}
              <p class="text-xs text-zinc-500">{detail?.manufacturer ?? part.manufacturer}</p>
            {/if}
            {#if detail?.package}
              <p class="text-xs font-mono text-zinc-500 ml-auto">{detail.package}</p>
            {/if}
          </div>
        {/if}

        <!-- Category -->
        {#if detail?.category_name}
          <p class="text-xs text-zinc-500">{detail.category_name}</p>
        {/if}

        <!-- Description -->
        {#if detail?.description ?? part.description}
          <p class="text-sm text-zinc-400">{detail?.description ?? part.description}</p>
        {/if}

        <!-- Links -->
        {#if detail?.datasheet_url || detail?.product_url}
          <div class="flex gap-4 pt-0.5">
            {#if detail.datasheet_url}
              <a href={detail.datasheet_url} target="_blank" rel="noopener noreferrer"
                 class="text-xs text-blue-400 underline active:text-blue-300">Datasheet ↗</a>
            {/if}
            {#if detail.product_url}
              <a href={detail.product_url} target="_blank" rel="noopener noreferrer"
                 class="text-xs text-blue-400 underline active:text-blue-300">Product Page ↗</a>
            {/if}
          </div>
        {/if}
      </div>

      <!-- Bag scan info (only when scanned from a bag) -->
      {#if part.sku || part.distributor_name}
        <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
          <p class="text-xs font-medium text-zinc-500 uppercase tracking-wide">From scan</p>
          {#if part.distributor_name}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Distributor</span>
              <span class="font-medium">{part.distributor_name}</span>
            </div>
          {/if}
          {#if part.sku}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">SKU</span>
              <span class="font-medium font-mono text-xs">{part.sku}</span>
            </div>
          {/if}
          {#if part.barcode_qty && part.barcode_qty > 0}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Bag Qty</span>
              <span class="font-medium">{part.barcode_qty}</span>
            </div>
          {/if}
        </div>
      {/if}

      <!-- Inventory -->
      <div class="rounded-xl bg-zinc-900 p-4 space-y-3">
        <div class="grid grid-cols-3 gap-3">
          <div>
            <p class="text-xs text-zinc-500 mb-0.5">In Stock</p>
            <p class="text-lg font-semibold tabular-nums">
              {detail?.quantity_on_hand ?? part.quantity_on_hand ?? 0}
            </p>
          </div>
          <div>
            <p class="text-xs text-zinc-500 mb-0.5">Reserved</p>
            <p class="text-lg font-semibold tabular-nums {detail && detail.quantity_reserved > 0 ? 'text-amber-400' : 'text-zinc-400'}">
              {detail?.quantity_reserved ?? 0}
            </p>
          </div>
          <div>
            <p class="text-xs text-zinc-500 mb-0.5">Available</p>
            <p class="text-lg font-semibold tabular-nums {detail && detail.quantity_available === 0 && (detail.quantity_on_hand ?? 0) > 0 ? 'text-red-400' : ''}">
              {detail?.quantity_available ?? part.quantity_on_hand ?? 0}
            </p>
          </div>
        </div>
        <!-- Location chip -->
        <div class="pt-2 border-t border-zinc-800">
          <p class="text-xs text-zinc-500 mb-1">Location</p>
          <button
            onclick={() => showLocationPicker = true}
            disabled={assigningLocation || !inventoryEntryId}
            class="flex w-full items-center gap-2 text-left active:opacity-70 disabled:opacity-50"
          >
            <!-- pin icon -->
            <svg class="h-4 w-4 shrink-0 text-zinc-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                d="M15 10.5a3 3 0 11-6 0 3 3 0 016 0z" />
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                d="M19.5 10.5c0 7.142-7.5 11.25-7.5 11.25S4.5 17.642 4.5 10.5a7.5 7.5 0 1115 0z" />
            </svg>
            {#if assigningLocation}
              <span class="text-sm text-zinc-500">Saving…</span>
            {:else if currentLocationLabel}
              <span class="text-sm font-medium truncate">{currentLocationLabel}</span>
            {:else}
              <span class="text-sm text-zinc-500">No location assigned</span>
            {/if}
            <svg class="h-4 w-4 shrink-0 text-zinc-600 ml-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
            </svg>
          </button>
        </div>
        {#if detail && (detail.minimum_stock_quantity != null || detail.reorder_quantity != null)}
          <div class="grid grid-cols-2 gap-3 pt-2 border-t border-zinc-800">
            <div>
              <p class="text-xs text-zinc-500 mb-0.5">Min Stock</p>
              <p class="text-sm font-medium">{detail.minimum_stock_quantity ?? '—'}</p>
            </div>
            <div>
              <p class="text-xs text-zinc-500 mb-0.5">Reorder Qty</p>
              <p class="text-sm font-medium">{detail.reorder_quantity ?? '—'}</p>
            </div>
          </div>
        {/if}
      </div>

      <!-- Adjust Stock -->
      {#if inventoryEntryId}
        <button
          onclick={adjustStock}
          class="w-full rounded-xl bg-orange-500/20 border border-orange-500/30 py-3 text-orange-300 font-medium active:bg-orange-500/30"
        >
          Adjust Stock
        </button>
      {:else}
        <p class="text-center text-xs text-zinc-500">Part not tracked in inventory</p>
      {/if}

      <!-- Listings -->
      {#if detail?.listings && detail.listings.length > 0}
        <div class="space-y-2">
          <p class="text-xs font-medium text-zinc-500 uppercase tracking-wide">Listings</p>
          {#each detail.listings as listing}
            <div class="rounded-xl bg-zinc-900 px-4 py-3">
              <div class="flex justify-between items-baseline">
                <span class="font-medium text-sm">{listing.distributor_name}</span>
                <span class="font-mono text-xs text-zinc-400">{listing.sku}</span>
              </div>
              <div class="flex gap-3 mt-1 text-xs text-zinc-400 flex-wrap">
                {#if listing.unit_price}
                  <PriceDisplay value={listing.unit_price} decimals={4} /> <span>each</span>
                {/if}
                {#if listing.stock_available != null}
                  <span>{listing.stock_available.toLocaleString()} in stock</span>
                {/if}
                {#if listing.moq > 1}
                  <span>MOQ {listing.moq}</span>
                {/if}
                {#if listing.packaging_type}
                  <span class="text-zinc-600">{listing.packaging_type}</span>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}

      <!-- Parameters -->
      {#if detail?.parameters && detail.parameters.length > 0}
        <div class="space-y-2">
          <p class="text-xs font-medium text-zinc-500 uppercase tracking-wide">Parameters</p>
          <div class="rounded-xl bg-zinc-900 p-4 grid grid-cols-2 gap-x-4 gap-y-2">
            {#each detail.parameters as param}
              <div>
                <p class="text-xs text-zinc-500 leading-tight">{param.name}</p>
                <p class="text-xs font-mono text-white">{param.value}{#if param.unit} {param.unit}{/if}</p>
              </div>
            {/each}
          </div>
        </div>
      {/if}

    </div>
  {/if}
</div>

{#if showLocationPicker}
  <MobileLocationPicker
    {locations}
    value={currentLocation}
    onSelect={(loc) => assignLocation(loc)}
    onClose={() => showLocationPicker = false}
  />
{/if}

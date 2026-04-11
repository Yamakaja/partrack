<script lang="ts">
  import { createScanner } from './scanner.svelte.js';
  import type { BagLookupResult, ScanPart } from './types.js';

  let {
    token,
    onBack,
    onAdjustStock,
    onSearchCatalog,
    onExpired,
  }: {
    token: string;
    onBack: () => void;
    onAdjustStock: (part: ScanPart) => void;
    onSearchCatalog: (query: string) => void;
    onExpired: () => void;
  } = $props();

  // ── Internal screen state ─────────────────────────────────────────────────

  type Screen = 'scanning' | 'found' | 'not_found';
  let screen = $state<Screen>('scanning');

  // ── State ─────────────────────────────────────────────────────────────────

  let bagPart = $state<BagLookupResult | null>(null);
  let bagParsedMPN = $state('');
  let bagParsedSKU = $state('');
  let bagParsedDistributor = $state('');
  let bagParsedQty = $state<number | null>(null);
  let bagParsedLot = $state('');
  let bagParseError = $state(false);
  let bagRaw = $state('');

  // ── Camera ────────────────────────────────────────────────────────────────

  let videoEl = $state<HTMLVideoElement | null>(null);
  const scanner = createScanner(() => videoEl);

  $effect(() => {
    if (screen === 'scanning' && videoEl) {
      scanner.startCamera();
      return () => scanner.stopCamera();
    }
  });

  // ── API ───────────────────────────────────────────────────────────────────

  async function apiFetch(method: string, path: string, body?: unknown): Promise<Response> {
    return fetch(path, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  }

  async function apiJSON<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await apiFetch(method, path, body);
    if (res.status === 401 || res.status === 410) {
      onExpired();
      throw new Error('expired');
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  // ── Scan handling ─────────────────────────────────────────────────────────

  async function handleBagScanResult(raw: string) {
    const data = await apiJSON<Record<string, unknown>>('POST', '/api/v1/scan/lookup', { barcode: raw });

    if (data.status === 'found') {
      bagPart = {
        id: data.id as string,
        mpn: (data.mpn as string) ?? null,
        manufacturer: (data.manufacturer as string) ?? null,
        description: (data.description as string) ?? null,
        sku: (data.sku as string) ?? null,
        distributor_name: (data.distributor_name as string) ?? null,
        distributor: (data.distributor as string) ?? null,
        barcode_qty: (data.barcode_qty as number) ?? 0,
        inventory_entry_id: (data.inventory_entry_id as string) ?? null,
        quantity_on_hand: (data.quantity_on_hand as number) ?? null,
        storage_location: (data.storage_location as string) ?? null,
      };
      screen = 'found';
    } else if (data.status === 'not_found') {
      bagParseError = false;
      bagParsedMPN = (data.parsed_mpn as string) || '';
      bagParsedSKU = (data.parsed_sku as string) || '';
      bagParsedDistributor = (data.parsed_distributor as string) || '';
      bagParsedQty = (data.parsed_qty as number) || null;
      bagParsedLot = (data.parsed_lot as string) || '';
      bagRaw = '';
      screen = 'not_found';
    } else {
      // parse_error — barcode format not recognised
      bagParseError = true;
      bagParsedMPN = '';
      bagParsedSKU = '';
      bagParsedDistributor = '';
      bagParsedQty = null;
      bagParsedLot = '';
      bagRaw = (data.raw as string) || '';
      screen = 'not_found';
    }
  }

  function handleAdjustStock() {
    if (!bagPart) return;
    onAdjustStock({
      id: bagPart.id,
      mpn: bagPart.mpn,
      description: bagPart.description,
      inventory_entry_id: bagPart.inventory_entry_id,
      quantity_on_hand: bagPart.quantity_on_hand ?? null,
      storage_location: bagPart.storage_location ?? null,
    });
  }
</script>

<!-- ── BAG SCANNING ───────────────────────────────────────────────────────── -->
{#if screen === 'scanning'}
  <div class="relative flex-1 overflow-hidden">
    <button
      onclick={onBack}
      aria-label="Back to menu"
      class="absolute top-3 left-3 z-10 flex h-8 w-8 items-center justify-center rounded-full bg-black/50 text-white"
    >
      <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>

    <!-- svelte-ignore a11y_media_has_caption -->
    <video bind:this={videoEl} autoplay playsinline muted class="h-full w-full object-cover"></video>

    {#if scanner.cameraError}
      <div class="absolute inset-0 flex flex-col items-center justify-center gap-4 bg-black/90 px-8 text-center">
        <svg class="h-12 w-12 text-zinc-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M15.75 10.5l4.72-4.72a.75.75 0 011.28.53v11.38a.75.75 0 01-1.28.53l-4.72-4.72M4.5 18.75h9a2.25 2.25 0 002.25-2.25v-9a2.25 2.25 0 00-2.25-2.25h-9A2.25 2.25 0 002.25 7.5v9a2.25 2.25 0 002.25 2.25z" />
          <line x1="3" y1="3" x2="21" y2="21" stroke-width="1.5" stroke-linecap="round" />
        </svg>
        <p class="text-sm text-zinc-300">{scanner.cameraError}</p>
      </div>
    {:else}
      <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
        <div class="h-56 w-56 rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
      </div>
      <div class="absolute bottom-0 left-0 right-0 flex justify-center pb-14">
        <button
          onclick={() => scanner.triggerScan(handleBagScanResult)}
          disabled={scanner.scanState === 'decoding'}
          class="px-14 py-5 rounded-full bg-white text-black font-semibold text-lg active:bg-zinc-200 disabled:opacity-60"
        >
          {#if scanner.scanState === 'decoding'}
            Scanning…
          {:else if scanner.scanState === 'no_barcode'}
            Not found — try again
          {:else}
            Scan
          {/if}
        </button>
      </div>
    {/if}
  </div>

<!-- ── BAG FOUND ──────────────────────────────────────────────────────────── -->
{:else if screen === 'found' && bagPart}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => { screen = 'scanning'; }} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Part Found</p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
        <p class="text-lg font-semibold">{bagPart.mpn ?? '—'}</p>
        {#if bagPart.manufacturer}
          <p class="text-xs text-zinc-500">{bagPart.manufacturer}</p>
        {/if}
        {#if bagPart.description}
          <p class="text-sm text-zinc-400">{bagPart.description}</p>
        {/if}
      </div>

      {#if bagPart.sku || bagPart.distributor_name}
        <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
          {#if bagPart.distributor_name}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Distributor</span>
              <span class="font-medium">{bagPart.distributor_name}</span>
            </div>
          {/if}
          {#if bagPart.sku}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">SKU</span>
              <span class="font-medium font-mono text-xs">{bagPart.sku}</span>
            </div>
          {/if}
          {#if bagPart.barcode_qty > 0}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Bag Qty</span>
              <span class="font-medium">{bagPart.barcode_qty}</span>
            </div>
          {/if}
        </div>
      {/if}

      <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
        <div class="flex justify-between text-sm">
          <span class="text-zinc-400">Stock</span>
          <span class="font-medium">{bagPart.inventory_entry_id ? (bagPart.quantity_on_hand ?? 0) : 'Not tracked'}</span>
        </div>
        {#if bagPart.storage_location}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">Location</span>
            <span class="font-medium">{bagPart.storage_location}</span>
          </div>
        {/if}
      </div>

      {#if bagPart.inventory_entry_id}
        <button
          onclick={handleAdjustStock}
          class="w-full rounded-xl bg-orange-500/20 border border-orange-500/30 py-3 text-orange-300 font-medium active:bg-orange-500/30"
        >
          Adjust Stock
        </button>
      {:else}
        <p class="text-center text-xs text-zinc-500">Part not tracked in inventory</p>
      {/if}

      <button
        onclick={() => { screen = 'scanning'; }}
        class="w-full rounded-xl border border-zinc-700 py-3 text-zinc-300 font-medium active:bg-zinc-800"
      >
        Scan Another
      </button>
    </div>
  </div>

<!-- ── BAG NOT FOUND ──────────────────────────────────────────────────────── -->
{:else if screen === 'not_found'}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => { screen = 'scanning'; }} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">{bagParseError ? 'Unrecognised barcode' : 'Part not found'}</p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      {#if bagParseError}
        <p class="text-sm text-zinc-400">
          The barcode was scanned successfully but its format is not supported. Only DigiKey and Mouser bag labels are supported.
        </p>
        {#if bagRaw}
          <div class="rounded-xl bg-zinc-900 p-4">
            <p class="text-xs font-medium text-zinc-500 uppercase tracking-wide mb-2">Raw barcode</p>
            <p class="text-xs font-mono text-zinc-300 break-all select-text">{bagRaw}</p>
          </div>
        {/if}
      {:else}
        <p class="text-sm text-zinc-400">
          The barcode was parsed but no matching part was found in your catalog. Fields read from the label:
        </p>
        <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
          {#if bagParsedDistributor}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Distributor</span>
              <span class="font-medium">{bagParsedDistributor}</span>
            </div>
          {/if}
          {#if bagParsedSKU}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">SKU</span>
              <span class="font-medium font-mono text-xs">{bagParsedSKU}</span>
            </div>
          {/if}
          {#if bagParsedMPN}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">MPN</span>
              <span class="font-medium font-mono text-xs">{bagParsedMPN}</span>
            </div>
          {/if}
          {#if bagParsedQty}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Qty</span>
              <span class="font-medium">{bagParsedQty}</span>
            </div>
          {/if}
          {#if bagParsedLot}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Lot / Date code</span>
              <span class="font-medium font-mono text-xs">{bagParsedLot}</span>
            </div>
          {/if}
          {#if !bagParsedDistributor && !bagParsedSKU && !bagParsedMPN}
            <p class="text-xs text-zinc-500">No fields could be extracted.</p>
          {/if}
        </div>
      {/if}

      <div class="flex flex-col gap-3">
        {#if !bagParseError && (bagParsedMPN || bagParsedSKU)}
          <button
            onclick={() => onSearchCatalog(bagParsedMPN || bagParsedSKU)}
            class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
          >
            Search Catalog
          </button>
        {/if}
        <button
          onclick={() => { screen = 'scanning'; }}
          class="w-full rounded-lg border border-zinc-700 py-3 text-base font-semibold text-zinc-300 active:bg-zinc-800"
        >
          Scan Another
        </button>
      </div>
    </div>
  </div>
{/if}

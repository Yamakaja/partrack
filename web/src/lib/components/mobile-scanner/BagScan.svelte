<script lang="ts">
  import { createScanner } from './scanner.svelte.js';
  import type { ScanPart } from './types.js';

  let {
    token,
    onBack,
    onPartFound,
    onPartImported,
    onSearchDistributor,
    onSearchCatalog,
    onExpired,
  }: {
    token: string;
    onBack: () => void;
    onPartFound: (part: ScanPart, bagQty: number, newEntry: boolean) => void;
    onPartImported: (part: ScanPart, bagQty: number) => void;
    onSearchDistributor: (distributorId: string, distributorName: string, query: string, parsedQty: number | null) => void;
    onSearchCatalog: (query: string) => void;
    onExpired: () => void;
  } = $props();

  // ── Internal screen state ─────────────────────────────────────────────────

  type Screen = 'scanning' | 'scan_result' | 'not_found_in_db' | 'importing';
  let screen = $state<Screen>('scanning');

  // ── State ─────────────────────────────────────────────────────────────────

  // scan_result state
  let parseError = $state(false);   // true = format unrecognised; false = parsed but no fields
  let rawBarcode = $state('');
  let parsedMPN = $state('');
  let parsedSKU = $state('');
  let parsedDistributor = $state('');
  let parsedQty = $state<number | null>(null);
  let parsedLot = $state('');

  // not_found_in_db / importing state (reuses parsed* variables above)
  let parsedManufacturer = $state('');
  let parsedDistributorId = $state('');
  let preferredDistributorId = $state('');
  let preferredDistributorName = $state('');
  let importError = $state('');

  // ── Camera ────────────────────────────────────────────────────────────────

  let videoEl = $state<HTMLVideoElement | null>(null);
  const scanner = createScanner(() => videoEl);

  $effect(() => {
    if (screen === 'scanning' && videoEl) {
      scanner.startCamera();
      return () => scanner.stopCamera();
    }
  });

  // ── Auto-import effect (Case A: SKU + configured distributor) ─────────────

  $effect(() => {
    if (screen !== 'importing') return;
    let cancelled = false;
    (async () => {
      try {
        const data = await apiJSON<{ part_id: string }>('POST', '/api/v1/scan/parts/import', {
          distributor_id: parsedDistributorId,
          sku: parsedSKU,
        });
        if (cancelled) return;
        const part: ScanPart = {
          id: data.part_id,
          mpn: parsedMPN || null,
          description: null,
          inventory_entry_id: null,
          quantity_on_hand: null,
          storage_location: null,
          manufacturer: parsedManufacturer || null,
          sku: parsedSKU || null,
          distributor_name: parsedDistributor || null,
        };
        onPartImported(part, parsedQty ?? 0);
      } catch {
        if (!cancelled) {
          importError = 'Import failed. Try again.';
          screen = 'not_found_in_db';
        }
      }
    })();
    return () => { cancelled = true; };
  });

  // ── Distributor search (Cases B/C/D) ──────────────────────────────────────

  function openDistributorSearch() {
    const distId   = parsedDistributorId || preferredDistributorId;
    const distName = parsedDistributorId ? parsedDistributor : preferredDistributorName;
    const query    = [parsedManufacturer, parsedMPN].filter(Boolean).join(' ') || parsedSKU;
    onSearchDistributor(distId, distName, query, parsedQty);
  }

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
      onPartFound(
        {
          id: data.id as string,
          mpn: (data.mpn as string) ?? null,
          description: (data.description as string) ?? null,
          inventory_entry_id: (data.inventory_entry_id as string) ?? null,
          quantity_on_hand: (data.quantity_on_hand as number) ?? null,
          storage_location: (data.storage_location as string) ?? null,
          manufacturer: (data.manufacturer as string) ?? null,
          sku: (data.sku as string) ?? null,
          distributor_name: (data.distributor_name as string) ?? null,
          distributor: (data.distributor as string) ?? null,
          barcode_qty: (data.barcode_qty as number) ?? 0,
        },
        (data.bag_qty as number) ?? 0,
        (data.inventory_entry_newly_created as boolean) ?? false,
      );
      // Screen stays 'scanning' — parent pushes the next screen onto the nav stack.

    } else if (data.status === 'not_found_in_db') {
      parseError = false;
      parsedMPN = (data.parsed_mpn as string) || '';
      parsedManufacturer = (data.parsed_manufacturer as string) || '';
      parsedSKU = (data.parsed_sku as string) || '';
      parsedDistributor = (data.parsed_distributor as string) || '';
      parsedDistributorId = (data.parsed_distributor_id as string) || '';
      preferredDistributorId = (data.preferred_distributor_id as string) || '';
      preferredDistributorName = (data.preferred_distributor_name as string) || '';
      parsedQty = (data.parsed_qty as number) || null;
      parsedLot = (data.parsed_lot as string) || '';
      importError = '';
      screen = data.can_auto_import ? 'importing' : 'not_found_in_db';

    } else if (data.status === 'no_fields') {
      parseError = false;
      parsedMPN = (data.parsed_mpn as string) || '';
      parsedSKU = (data.parsed_sku as string) || '';
      parsedDistributor = (data.parsed_distributor as string) || '';
      parsedQty = (data.parsed_qty as number) || null;
      parsedLot = (data.parsed_lot as string) || '';
      rawBarcode = '';
      screen = 'scan_result';

    } else {
      // parse_error — barcode format not recognised
      parseError = true;
      parsedMPN = '';
      parsedSKU = '';
      parsedDistributor = '';
      parsedQty = null;
      parsedLot = '';
      rawBarcode = (data.raw as string) || '';
      screen = 'scan_result';
    }
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

<!-- ── SCAN RESULT (parse_error / no_fields) ─────────────────────────────── -->
{:else if screen === 'scan_result'}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => { screen = 'scanning'; }} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">{parseError ? 'Unrecognised barcode' : 'No part info found'}</p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      {#if parseError}
        <p class="text-sm text-zinc-400">
          The barcode was scanned successfully but its format is not supported. Only DigiKey and Mouser bag labels are supported.
        </p>
        {#if rawBarcode}
          <div class="rounded-xl bg-zinc-900 p-4">
            <p class="text-xs font-medium text-zinc-500 uppercase tracking-wide mb-2">Raw barcode</p>
            <p class="text-xs font-mono text-zinc-300 break-all select-text">{rawBarcode}</p>
          </div>
        {/if}
      {:else}
        <p class="text-sm text-zinc-400">
          The barcode was parsed but no SKU or MPN could be extracted to identify a part.
        </p>
        {#if parsedDistributor || parsedLot}
          <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
            {#if parsedDistributor}
              <div class="flex justify-between text-sm">
                <span class="text-zinc-400">Distributor</span>
                <span class="font-medium">{parsedDistributor}</span>
              </div>
            {/if}
            {#if parsedLot}
              <div class="flex justify-between text-sm">
                <span class="text-zinc-400">Lot / Date code</span>
                <span class="font-medium font-mono text-xs">{parsedLot}</span>
              </div>
            {/if}
          </div>
        {/if}
      {/if}

      <button
        onclick={() => { screen = 'scanning'; }}
        class="w-full rounded-lg border border-zinc-700 py-3 text-base font-semibold text-zinc-300 active:bg-zinc-800"
      >
        Try Again
      </button>
    </div>
  </div>

<!-- ── NOT FOUND IN DB (part info found but not in catalog) ──────────────── -->
{:else if screen === 'not_found_in_db'}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => { screen = 'scanning'; }} aria-label="Back" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Part not found</p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <p class="text-sm text-zinc-400">
        This part isn't in your catalog yet.
      </p>

      <!-- Parsed barcode info -->
      <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
        {#if parsedDistributor}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">Distributor</span>
            <span class="font-medium">{parsedDistributor}</span>
          </div>
        {/if}
        {#if parsedSKU}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">SKU</span>
            <span class="font-medium font-mono text-xs">{parsedSKU}</span>
          </div>
        {/if}
        {#if parsedMPN}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">MPN</span>
            <span class="font-medium font-mono text-xs">{parsedMPN}</span>
          </div>
        {/if}
        {#if parsedManufacturer}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">Manufacturer</span>
            <span class="font-medium text-xs">{parsedManufacturer}</span>
          </div>
        {/if}
        {#if parsedQty}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">Qty</span>
            <span class="font-medium">{parsedQty}</span>
          </div>
        {/if}
        {#if parsedLot}
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">Lot / Date code</span>
            <span class="font-medium font-mono text-xs">{parsedLot}</span>
          </div>
        {/if}
      </div>

      {#if importError}
        <p class="text-xs text-red-400 text-center">{importError}</p>
      {/if}

      <div class="flex flex-col gap-3">
        <!-- Cases B/C/D: MPN available + a distributor to search -->
        {#if parsedMPN && (parsedDistributorId || preferredDistributorId)}
          {@const distName = parsedDistributorId ? parsedDistributor : preferredDistributorName}
          <button
            onclick={openDistributorSearch}
            class="w-full rounded-xl bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
          >
            Search {distName} for {parsedMPN}
          </button>
        {/if}

        {#if parsedMPN || parsedSKU}
          <button
            onclick={() => onSearchCatalog(parsedMPN || parsedSKU)}
            class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
          >
            Search Catalog
          </button>
        {/if}
        <button
          onclick={() => { screen = 'scanning'; }}
          class="w-full rounded-lg border border-zinc-700 py-3 text-base font-semibold text-zinc-300 active:bg-zinc-800"
        >
          Try Again
        </button>
      </div>
    </div>
  </div>

<!-- ── IMPORTING (Case A: auto-import in progress) ────────────────────────── -->
{:else if screen === 'importing'}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={() => { screen = 'not_found_in_db'; }} aria-label="Cancel" class="text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium">Importing Part</p>
    </div>
    <div class="flex flex-1 flex-col items-center justify-center gap-4 px-8 text-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
      <div>
        <p class="font-medium">Importing from {parsedDistributor || 'distributor'}…</p>
        <p class="mt-1 text-sm text-zinc-400">This may take a few seconds.</p>
      </div>
    </div>
  </div>
{/if}

<script lang="ts">
  import ScannerCamera from './ScannerCamera.svelte';
  import type { ScanPart, ScanImportContext, PartLocation } from './types.js';
  import { lookupBag, importScanPart, ScanExpiredError } from '$lib/api/scanner.js';
  import { parseStorageLocationQR } from '$lib/utils/ipc2d.js';

  let {
    token,
    onBack,
    onPartFound,
    onPartImported,
    onStartImport,
    onLocationScanned,
    onExpired,
  }: {
    token: string;
    onBack: () => void;
    onPartFound: (part: ScanPart, bagQty: number, newEntry: boolean) => void;
    onPartImported: (part: ScanPart, bagQty: number) => void;
    onStartImport: (context: ScanImportContext) => void;
    onLocationScanned: (loc: PartLocation) => void;
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
  let canAutoImport = $state(false);
  let distributorConfigs = $state<Array<{ id: string; name: string }>>([]);
  let importError = $state('');

  // ── Auto-import effect (Case A: fast path) ────────────────────────────────

  $effect(() => {
    if (screen !== 'importing') return;
    let cancelled = false;
    (async () => {
      try {
        const data = await importScanPart(token, { distributor_id: parsedDistributorId, sku: parsedSKU });
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
      } catch (e) {
        if (cancelled) return;
        if (e instanceof ScanExpiredError) { onExpired(); return; }
        importError = 'Import failed. Try again.';
        screen = 'not_found_in_db';
      }
    })();
    return () => { cancelled = true; };
  });

  // ── Start multi-listing import flow ───────────────────────────────────────

  function startImport() {
    onStartImport({
      parsedMPN,
      parsedManufacturer,
      parsedSKU,
      parsedDistributorId,
      parsedDistributorName: parsedDistributor,
      preferredDistributorId,
      preferredDistributorName,
      parsedQty,
      canAutoImport,
      distributorConfigs,
    });
  }

  // ── Scan handling ─────────────────────────────────────────────────────────

  async function handleBagScanResult(raw: string) {
    // Location QR takes priority — don't hit the bag lookup endpoint.
    const loc = parseStorageLocationQR(raw);
    if (loc) {
      onLocationScanned(loc);
      return;
    }

    try {
      const data = await lookupBag(token, raw);

      if (data.status === 'found') {
        onPartFound(
          {
            id: data.id,
            mpn: data.mpn,
            description: data.description,
            inventory_entry_id: data.inventory_entry_id,
            quantity_on_hand: data.quantity_on_hand,
            storage_location: data.storage_location,
            manufacturer: data.manufacturer,
            sku: data.sku,
            distributor_name: data.distributor_name,
            distributor: data.distributor,
            barcode_qty: data.barcode_qty,
          },
          data.bag_qty,
          data.inventory_entry_newly_created,
        );
        // Screen stays 'scanning' — parent pushes the next screen onto the nav stack.

      } else if (data.status === 'not_found_in_db') {
        parseError = false;
        parsedMPN = data.parsed_mpn;
        parsedManufacturer = data.parsed_manufacturer;
        parsedSKU = data.parsed_sku;
        parsedDistributor = data.parsed_distributor;
        parsedDistributorId = data.parsed_distributor_id;
        preferredDistributorId = data.preferred_distributor_id;
        preferredDistributorName = data.preferred_distributor_name;
        parsedQty = data.parsed_qty;
        parsedLot = data.parsed_lot;
        canAutoImport = data.can_auto_import;
        distributorConfigs = data.distributor_configs ?? [];
        importError = '';

        // Fast path: SKU known and no second distributor to offer → auto-import immediately.
        const isFastPath = data.can_auto_import &&
          (!data.preferred_distributor_id || data.parsed_distributor_id === data.preferred_distributor_id);
        screen = isFastPath ? 'importing' : 'not_found_in_db';

      } else if (data.status === 'no_fields') {
        parseError = false;
        parsedMPN = data.parsed_mpn;
        parsedSKU = data.parsed_sku;
        parsedDistributor = data.parsed_distributor;
        parsedQty = data.parsed_qty;
        parsedLot = data.parsed_lot;
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
        rawBarcode = data.raw || '';
        screen = 'scan_result';
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    }
  }
</script>

<!-- ── BAG SCANNING ───────────────────────────────────────────────────────── -->
{#if screen === 'scanning'}
  <ScannerCamera onBack={onBack} onScan={handleBagScanResult} />

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
        {#if parsedMPN || parsedSKU}
          <button
            onclick={startImport}
            class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200"
          >
            Review & Import
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

<!-- ── IMPORTING (Case A: fast-path auto-import in progress) ──────────────── -->
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

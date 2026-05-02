<script lang="ts">
  import { onMount } from 'svelte';
  import ScannerCamera from './ScannerCamera.svelte';
  import type { ScanPart, ScanImportContext, PartLocation } from './types.js';
  import {
    lookupBag, importScanPart, ScanExpiredError,
    checkSession, startBulkSession, addBulkScanItem, patchBulkScanItem,
    closeBulkSession, navigateBulkSession,
  } from '$lib/api/scanner.js';
  import { parseStorageLocationQR } from '$lib/utils/ipc2d.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

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
  let parseError = $state(false);
  let rawBarcode = $state('');
  let parsedMPN = $state('');
  let parsedSKU = $state('');
  let parsedDistributor = $state('');
  let parsedQty = $state<number | null>(null);
  let parsedLot = $state('');

  // not_found_in_db / importing state
  let parsedManufacturer = $state('');
  let parsedDistributorId = $state('');
  let preferredDistributorId = $state('');
  let preferredDistributorName = $state('');
  let canAutoImport = $state(false);
  let distributorConfigs = $state<Array<{ id: string; name: string }>>([]);
  let importError = $state('');

  // ── Bulk mode state ───────────────────────────────────────────────────────

  let bulkMode = $state(false);
  let bulkSessionId = $state<string | null>(null);
  let bulkItemCount = $state(0);
  // Map<barcode, lastSeenAt ms> — resets on component destroy
  const seenBarcodes = new Map<string, number>();

  // Resume dialog
  let showResumeDialog = $state(false);
  let resumeSessionId = $state<string | null>(null);
  let resumeItemCount = $state(0);

  // Qty capture overlay
  let showQtyOverlay = $state(false);
  let qtyOverlayItemId = $state<string | null>(null);
  let qtyOverlaySessionId = $state<string | null>(null);
  let qtyInput = $state('');

  // ── Svelte action: focus element when mounted ─────────────────────────────

  function focusOnMount(node: HTMLElement) {
    node.focus();
  }

  // ── On mount: check for existing bulk session to resume ───────────────────

  onMount(() => {
    checkSession(token).then((s) => {
      if (!s.bulk_session) return;
      if (s.bulk_session.state === 'active') {
        // Auto-resume silently
        bulkMode = true;
        bulkSessionId = s.bulk_session.id;
        bulkItemCount = s.bulk_session.item_count;
      } else if (s.bulk_session.state === 'timed_out') {
        resumeSessionId = s.bulk_session.id;
        resumeItemCount = s.bulk_session.item_count;
        showResumeDialog = true;
      }
    }).catch(() => {});

    return () => {
      showQtyOverlay = false;
    };
  });

  // ── Resume dialog actions ─────────────────────────────────────────────────

  function handleResumeAccept() {
    showResumeDialog = false;
    if (!resumeSessionId) return;
    bulkMode = true;
    bulkSessionId = resumeSessionId;
    bulkItemCount = resumeItemCount;
    resumeSessionId = null;
  }

  async function handleResumeDismiss() {
    showResumeDialog = false;
    if (resumeSessionId) {
      closeBulkSession(token, resumeSessionId).catch(() => {});
    }
    resumeSessionId = null;
  }

  // ── Bulk mode toggle ──────────────────────────────────────────────────────

  async function enableBulkMode() {
    try {
      const res = await startBulkSession(token);
      bulkSessionId = res.session_id;
      bulkItemCount = res.item_count;
      bulkMode = true;
      seenBarcodes.clear();
      pushToast('success', 'Bulk mode enabled');
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Could not start bulk session');
    }
  }

  async function disableBulkMode() {
    bulkMode = false;
    if (bulkSessionId) {
      closeBulkSession(token, bulkSessionId).catch(() => {});
    }
    bulkSessionId = null;
    bulkItemCount = 0;
    seenBarcodes.clear();
    showQtyOverlay = false;
    pushToast('info', 'Bulk mode disabled');
  }

  // ── Qty overlay helpers ───────────────────────────────────────────────────

  function showQtyCaptureFor(itemId: string, sessionId: string, encodedQty: number | null) {
    qtyOverlayItemId = itemId;
    qtyOverlaySessionId = sessionId;
    qtyInput = encodedQty != null ? String(encodedQty) : '';
    showQtyOverlay = true;
  }

  function submitQty(qty: number | null) {
    showQtyOverlay = false;
    const itemId = qtyOverlayItemId;
    const sid = qtyOverlaySessionId;
    qtyOverlayItemId = null;
    qtyOverlaySessionId = null;
    if (itemId && sid) {
      patchBulkScanItem(token, sid, itemId, { initial_qty: qty }).catch(() => {});
    }
  }

  function handleQtyConfirm() {
    const n = parseInt(qtyInput, 10);
    submitQty(isNaN(n) || n < 0 ? null : n);
  }

  function handleQtySkip() {
    submitQty(null);
  }

  // ── Navigate to desktop review ────────────────────────────────────────────

  async function handleNavigateReview() {
    if (!bulkSessionId) return;
    try {
      await navigateBulkSession(token, bulkSessionId);
      pushToast('success', 'Desktop navigated to review');
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      // 409 = desktop offline — show fallback toast with guidance
      pushToast('warning', 'Desktop is offline — open the review page manually');
    }
  }

  // ── Bulk scan barcode handler ─────────────────────────────────────────────

  async function handleBulkScan(raw: string) {
    if (!bulkSessionId) return;
    const now = Date.now();
    const lastSeen = seenBarcodes.get(raw);
    seenBarcodes.set(raw, now);

    if (lastSeen !== undefined) {
      if (now - lastSeen <= 3000) {
        pushToast('info', 'Scanned');
      } else {
        pushToast('warning', 'Already scanned!');
      }
      return;
    }

    try {
      const res = await addBulkScanItem(token, bulkSessionId, raw);
      if (res.new) {
        bulkItemCount++;
        pushToast('success', res.found_in_db ? 'Scanned' : 'Scanned — looking up part…');
        showQtyCaptureFor(res.item_id, bulkSessionId, res.encoded_qty);
      } else {
        pushToast('info', 'Scanned');
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      pushToast('error', 'Scan failed');
    }
  }

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

  // ── Normal scan handling ──────────────────────────────────────────────────

  async function handleBagScanResult(raw: string) {
    if (bulkMode) {
      await handleBulkScan(raw);
      return;
    }

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

<!-- ── RESUME DIALOG ───────────────────────────────────────────────────────── -->
{#if showResumeDialog}
  <div class="absolute inset-0 z-50 flex items-center justify-center bg-black/80 px-6">
    <div class="w-full max-w-sm rounded-2xl bg-zinc-900 p-6 space-y-4">
      <h2 class="text-base font-semibold">Resume bulk session?</h2>
      <p class="text-sm text-zinc-400">
        You have an unfinished bulk scan session with {resumeItemCount} item{resumeItemCount === 1 ? '' : 's'} pending review.
      </p>
      <div class="flex gap-3">
        <button
          onclick={handleResumeDismiss}
          class="flex-1 rounded-lg border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
        >
          Discard
        </button>
        <button
          onclick={handleResumeAccept}
          class="flex-1 rounded-lg bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
        >
          Resume
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ── BAG SCANNING ───────────────────────────────────────────────────────── -->
{#if screen === 'scanning'}
  <div class="relative flex flex-1 flex-col overflow-hidden">
    <ScannerCamera {onBack} onScan={handleBagScanResult}>
      {#snippet topRightControls()}
        {#if bulkMode && bulkItemCount > 0}
          <button
            onclick={handleNavigateReview}
            class="rounded-full bg-black/50 px-3 py-1.5 text-xs font-semibold text-white active:bg-black/70"
          >
            Review ({bulkItemCount})
          </button>
        {/if}
        {#if !bulkMode}
          <button
            onclick={enableBulkMode}
            class="flex h-8 w-8 items-center justify-center rounded-full bg-black/50 text-white/80 active:bg-black/70"
            title="Enable bulk mode"
          >
            <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M4 6h16M4 10h16M4 14h16M4 18h16" />
            </svg>
          </button>
        {:else}
          <button
            onclick={disableBulkMode}
            class="flex h-8 w-8 items-center justify-center rounded-full bg-blue-600/80 text-white active:bg-blue-700"
            title="Disable bulk mode"
          >
            <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M4 6h16M4 10h16M4 14h16M4 18h16" />
            </svg>
          </button>
        {/if}
      {/snippet}
    </ScannerCamera>

    <!-- Qty capture overlay (bulk mode, unknown part) -->
    {#if showQtyOverlay}
      <div class="absolute inset-x-0 bottom-0 z-30 rounded-t-2xl bg-zinc-900 p-6 space-y-4">
        <p class="text-sm font-semibold">How many in this bag?</p>
        <input
          type="number"
          min="0"
          bind:value={qtyInput}
          placeholder="Enter quantity"
          class="w-full rounded-lg bg-zinc-800 px-4 py-3 text-base text-white placeholder-zinc-500 focus:outline-none"
          use:focusOnMount
        />
        <div class="flex gap-3">
          <button
            onclick={handleQtySkip}
            class="flex-1 rounded-lg border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
          >
            Skip
          </button>
          <button
            onclick={handleQtyConfirm}
            class="flex-1 rounded-lg bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
          >
            Confirm
          </button>
        </div>
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

<!-- ── NOT FOUND IN DB ────────────────────────────────────────────────────── -->
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

<!-- ── IMPORTING ──────────────────────────────────────────────────────────── -->
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

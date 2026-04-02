<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  // ── State ────────────────────────────────────────────────────────────────

  type ScanState =
    | 'loading'
    | 'no_context'
    | 'scanning'
    | 'confirming'
    | 'confirming_over_receive'
    | 'error_no_match'
    | 'error_parse'
    | 'expired';

  let scanState = $state<ScanState>('loading');
  let token = $state('');

  let contextOrderRef = $state<string | null>(null);
  let confirmData = $state<{
    lineItemId: string;
    partName: string;
    mpn: string;
    sku: string;
    barcodeQty: number;
    quantityOrdered: number;
    alreadyReceived: number;
    overReceive: boolean;
  } | null>(null);
  let confirmQty = $state('');
  let submitting = $state(false);
  let errorMessage = $state('');

  // ── Auth helper ──────────────────────────────────────────────────────────

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

  // ── Polling ──────────────────────────────────────────────────────────────

  let pollInterval: ReturnType<typeof setInterval> | null = null;

  async function pollSession() {
    if (!token) return;
    try {
      const res = await apiFetch('GET', '/api/v1/scan/session');
      if (res.status === 401 || res.status === 410) {
        scanState = 'expired';
        stopPolling();
        stopCamera();
        return;
      }
      if (!res.ok) return;

      const data = await res.json();
      contextOrderRef = data.context_order_ref ?? null;

      if (scanState === 'loading') {
        scanState = data.context_type ? 'scanning' : 'no_context';
      } else if (scanState === 'no_context' && data.context_type) {
        scanState = 'scanning';
      } else if (scanState === 'scanning' && !data.context_type) {
        scanState = 'no_context';
        stopCamera();
      }

      // Recovery: if pending_scan arrives via poll
      if (data.pending_scan && (scanState === 'scanning' || scanState === 'no_context')) {
        applyPendingScan(data.pending_scan);
      }
    } catch {
      // network error — keep polling
    }
  }

  function startPolling() {
    pollSession();
    pollInterval = setInterval(pollSession, 2000);
  }

  function stopPolling() {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  // ── Camera / ZXing ───────────────────────────────────────────────────────

  let videoEl = $state<HTMLVideoElement | null>(null);
  let scannerControls: { stop: () => void } | null = null;

  async function startCamera() {
    if (!videoEl) return;
    try {
      const { BrowserMultiFormatReader } = await import('@zxing/browser');
      const reader = new BrowserMultiFormatReader();
      const controls = await reader.decodeFromVideoDevice(undefined, videoEl, async (result, _err) => {
        if (result && scanState === 'scanning') {
          await handleScanResult(result.getText());
        }
      });
      scannerControls = controls;
    } catch (e) {
      console.error('Camera init error:', e);
    }
  }

  function stopCamera() {
    if (scannerControls) {
      try { scannerControls.stop(); } catch {}
      scannerControls = null;
    }
  }

  $effect(() => {
    if (scanState === 'scanning' && videoEl && !scannerControls) {
      startCamera();
    } else if (scanState !== 'scanning' && scannerControls) {
      stopCamera();
    }
  });

  // ── Scan handling ────────────────────────────────────────────────────────

  async function handleScanResult(raw: string) {
    stopCamera();
    try {
      const res = await apiFetch('POST', '/api/v1/scan/scans', { barcode: raw });
      if (res.status === 401 || res.status === 410) { scanState = 'expired'; return; }
      const data = await res.json();

      if (data.status === 'matched') {
        applyPendingScan(data);
      } else if (data.status === 'no_match') {
        scanState = 'error_no_match';
        errorMessage = `Part not found in this order (MPN: ${data.parsed_mpn || '—'}, SKU: ${data.parsed_sku || '—'})`;
      } else {
        scanState = 'error_parse';
        errorMessage = 'Unrecognized barcode format';
      }
    } catch {
      scanState = 'scanning';
      startCamera();
    }
  }

  function applyPendingScan(data: Record<string, unknown>) {
    confirmData = {
      lineItemId: data.line_item_id as string,
      partName: data.part_name as string,
      mpn: data.mpn as string,
      sku: data.sku as string,
      barcodeQty: data.barcode_qty as number,
      quantityOrdered: data.quantity_ordered as number,
      alreadyReceived: data.already_received as number,
      overReceive: data.over_receive as boolean,
    };
    confirmQty = String(data.barcode_qty || '');
    scanState = data.over_receive ? 'confirming_over_receive' : 'confirming';
  }

  async function confirm() {
    if (submitting) return;
    const qty = parseInt(confirmQty, 10);
    if (isNaN(qty) || qty <= 0) return;
    submitting = true;
    try {
      const res = await apiFetch('POST', '/api/v1/scan/confirm', { qty });
      if (res.status === 401 || res.status === 410) { scanState = 'expired'; return; }
      confirmData = null;
      scanState = 'scanning';
      startCamera();
    } catch {
      // keep state for retry
    } finally {
      submitting = false;
    }
  }

  async function skip() {
    apiFetch('POST', '/api/v1/scan/skip').catch(() => {});
    confirmData = null;
    scanState = 'scanning';
    startCamera();
  }

  function resumeScanning() {
    scanState = 'scanning';
    startCamera();
  }

  // ── Lifecycle ────────────────────────────────────────────────────────────

  onMount(() => {
    const params = new URLSearchParams(window.location.search);
    token = params.get('token') ?? '';
    if (!token) {
      scanState = 'expired';
      return;
    }
    startPolling();
  });

  onDestroy(() => {
    stopPolling();
    stopCamera();
  });
</script>

<div class="flex h-screen flex-col bg-black text-white overflow-hidden select-none">

  <!-- Context banner -->
  {#if scanState !== 'expired' && scanState !== 'loading'}
    <div class="px-4 py-2 bg-zinc-900 text-center text-xs text-zinc-400 shrink-0">
      {#if contextOrderRef}
        Receiving order <span class="text-white font-medium">{contextOrderRef}</span>
      {:else}
        Waiting for desktop context…
      {/if}
    </div>
  {/if}

  <!-- Loading -->
  {#if scanState === 'loading'}
    <div class="flex flex-1 flex-col items-center justify-center gap-3">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
      <p class="text-sm text-zinc-400">Connecting…</p>
    </div>

  <!-- Expired / invalid -->
  {:else if scanState === 'expired'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-5xl">⏱</div>
      <div>
        <p class="text-lg font-semibold">Session expired</p>
        <p class="mt-2 text-sm text-zinc-400">Ask the desktop user to share a new QR code to continue scanning.</p>
      </div>
    </div>

  <!-- No context -->
  {:else if scanState === 'no_context'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-5xl">📱</div>
      <div>
        <p class="text-lg font-semibold">Scanner ready</p>
        <p class="mt-2 text-sm text-zinc-400">Open an order on the desktop to start receiving parts.</p>
      </div>
    </div>

  <!-- Scanning — camera view -->
  {:else if scanState === 'scanning'}
    <div class="relative flex-1 overflow-hidden">
      <!-- svelte-ignore a11y_media_has_caption -->
      <video
        bind:this={videoEl}
        autoplay
        playsinline
        muted
        class="h-full w-full object-cover"
      ></video>
      <!-- Viewfinder overlay -->
      <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
        <div class="h-56 w-56 rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
      </div>
      <p class="absolute bottom-6 left-0 right-0 text-center text-xs text-white/60">
        Point the camera at a bag barcode
      </p>
    </div>

  <!-- No match -->
  {:else if scanState === 'error_no_match'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-5xl">❌</div>
      <div>
        <p class="text-lg font-semibold">Not in this order</p>
        <p class="mt-2 text-sm text-zinc-400">{errorMessage}</p>
      </div>
      <button
        onclick={resumeScanning}
        class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Scan Next
      </button>
    </div>

  <!-- Parse error -->
  {:else if scanState === 'error_parse'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-5xl">⚠️</div>
      <div>
        <p class="text-lg font-semibold">Unrecognized barcode format</p>
        <p class="mt-2 text-sm text-zinc-400">Only DigiKey and Mouser bag labels are supported.</p>
      </div>
      <button
        onclick={resumeScanning}
        class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Scan Next
      </button>
    </div>

  <!-- Confirmation panel -->
  {:else if scanState === 'confirming' || scanState === 'confirming_over_receive'}
    <div class="flex flex-1 flex-col justify-end">
      <div class="rounded-t-2xl bg-zinc-900 px-5 pt-5 pb-8 space-y-4">

        <!-- Over-receive warning -->
        {#if scanState === 'confirming_over_receive'}
          <div class="rounded-lg bg-amber-950 border border-amber-700 px-4 py-3 text-sm text-amber-300">
            <span class="font-semibold">Over-receive:</span>
            Bag has {confirmData?.barcodeQty} but only {(confirmData?.quantityOrdered ?? 0) - (confirmData?.alreadyReceived ?? 0)} remaining on this line item.
          </div>
        {/if}

        <!-- Part info -->
        <div>
          <p class="text-base font-semibold leading-snug">{confirmData?.partName || '—'}</p>
          <p class="mt-0.5 text-sm text-zinc-400">
            {#if confirmData?.mpn}MPN: {confirmData.mpn}{/if}
            {#if confirmData?.mpn && confirmData?.sku} · {/if}
            {#if confirmData?.sku}SKU: {confirmData.sku}{/if}
          </p>
        </div>

        <!-- Quantity input -->
        <div>
          <p class="mb-1 text-xs font-medium text-zinc-400 uppercase tracking-wide">Quantity</p>
          <div class="flex items-center gap-2">
            <input
              type="number"
              bind:value={confirmQty}
              min="1"
              inputmode="numeric"
              class="w-28 rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-lg font-mono text-white focus:outline-none focus:ring-2 focus:ring-white/30"
            />
            <span class="text-sm text-zinc-400">pcs</span>
          </div>
        </div>

        <!-- Line item info -->
        <p class="text-sm text-zinc-400">
          Ordered: {confirmData?.quantityOrdered} · Received: {confirmData?.alreadyReceived}
        </p>

        <!-- Action buttons -->
        <div class="flex gap-3 pt-1">
          <button
            onclick={confirm}
            disabled={submitting}
            class="flex-1 rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : 'Confirm'}
          </button>
          <button
            onclick={skip}
            class="flex-1 rounded-lg border border-zinc-700 py-3 text-base font-semibold text-zinc-300 active:bg-zinc-800"
          >
            Skip
          </button>
        </div>

      </div>
    </div>

  {/if}

</div>

<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  // ── Screen state machine ─────────────────────────────────────────────────

  type Screen =
    | 'loading'
    | 'expired'
    | 'menu'
    | 'order_list'
    | 'scanning'
    | 'confirming'
    | 'confirming_over_receive'
    | 'order_complete'
    | 'error_no_match'
    | 'error_parse'
    | 'find_part_search'
    | 'find_part_result'
    | 'stock_adj_search'
    | 'stock_adj_qty'
    | 'stock_adj_done';

  let screen = $state<Screen>('loading');
  let token = $state('');

  // ── Context ───────────────────────────────────────────────────────────────

  let contextOrderId = $state<string | null>(null);
  let contextOrderRef = $state<string | null>(null);

  // ── Order list ────────────────────────────────────────────────────────────

  interface ScanOrder {
    id: string;
    ref: string;
    status: string;
    distributor_name: string;
    item_count: number;
  }
  let orders = $state<ScanOrder[]>([]);
  let ordersLoading = $state(false);

  // ── Scanning / confirmation ───────────────────────────────────────────────

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
  let errorMPN = $state('');
  let errorSKU = $state('');

  // ── Find Part ─────────────────────────────────────────────────────────────

  interface ScanPart {
    id: string;
    mpn: string | null;
    description: string | null;
    inventory_entry_id: string | null;
    quantity_on_hand: number | null;
    storage_location: string | null;
  }
  let partSearchQ = $state('');
  let partResults = $state<ScanPart[]>([]);
  let partSearchLoading = $state(false);
  let selectedPart = $state<ScanPart | null>(null);
  let partSearchDebounce: ReturnType<typeof setTimeout> | null = null;

  // ── Stock adjustment ──────────────────────────────────────────────────────

  let adjPart = $state<ScanPart | null>(null);
  let adjSearchQ = $state('');
  let adjResults = $state<ScanPart[]>([]);
  let adjSearchLoading = $state(false);
  let adjSearchDebounce: ReturnType<typeof setTimeout> | null = null;
  let adjDelta = $state('-1');
  let adjReason = $state('');
  let adjSubmitting = $state(false);
  let adjNewQty = $state<number | null>(null);

  // ── Auth helper ───────────────────────────────────────────────────────────

  const TOKEN_KEY = 'scan_token';

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
      handleExpired();
      throw new Error('expired');
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  function handleExpired() {
    localStorage.removeItem(TOKEN_KEY);
    token = '';
    stopCamera();
    stopPolling();
    screen = 'expired';
  }

  // ── Phone SSE ─────────────────────────────────────────────────────────────

  let phoneES: EventSource | null = null;

  function openPhoneSSE(t: string) {
    if (phoneES) {
      phoneES.close();
    }
    phoneES = new EventSource(`/api/v1/scan/stream?token=${encodeURIComponent(t)}`);
    phoneES.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as Record<string, unknown>;
        if (msg.type === 'scan_context_changed') {
          contextOrderId = (msg.context_order_id as string) ?? null;
          contextOrderRef = (msg.context_order_ref as string) ?? null;
          // If on menu and context arrived, show context banner — no auto-transition
        }
      } catch {}
    };
    // EventSource auto-reconnects on error — no action needed.
  }

  // ── Polling (fallback for context recovery) ───────────────────────────────

  let pollInterval: ReturnType<typeof setInterval> | null = null;

  async function pollSession() {
    if (!token) return;
    try {
      const res = await apiFetch('GET', '/api/v1/scan/session');
      if (res.status === 401 || res.status === 410) {
        handleExpired();
        return;
      }
      if (!res.ok) return;
      const data = await res.json();
      contextOrderId = data.context_order_id ?? null;
      contextOrderRef = data.context_order_ref ?? null;
      if (screen === 'loading') {
        screen = 'menu';
      }
    } catch {
      // network error — keep polling
    }
  }

  function startPolling() {
    pollSession();
    pollInterval = setInterval(pollSession, 10000);
  }

  function stopPolling() {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  // ── Camera / ZXing ────────────────────────────────────────────────────────

  let videoEl = $state<HTMLVideoElement | null>(null);
  let scannerControls: { stop: () => void } | null = null;

  async function startCamera() {
    if (!videoEl) return;
    try {
      const { BrowserMultiFormatReader } = await import('@zxing/browser');
      const reader = new BrowserMultiFormatReader();
      const controls = await reader.decodeFromVideoDevice(undefined, videoEl, async (result, _err) => {
        if (result && screen === 'scanning') {
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
    const scanning = screen === 'scanning';
    if (scanning && videoEl && !scannerControls) {
      startCamera();
    } else if (!scanning && scannerControls) {
      stopCamera();
    }
  });

  // ── Scan handling ─────────────────────────────────────────────────────────

  async function handleScanResult(raw: string) {
    stopCamera();
    try {
      const data = await apiJSON<Record<string, unknown>>('POST', '/api/v1/scan/scans', { barcode: raw });

      if (data.status === 'matched') {
        applyPendingScan(data);
      } else if (data.status === 'no_match') {
        screen = 'error_no_match';
        errorMessage = `Part not found in this order`;
        errorMPN = (data.parsed_mpn as string) || '';
        errorSKU = (data.parsed_sku as string) || '';
      } else {
        screen = 'error_parse';
        errorMessage = 'Unrecognized barcode format';
      }
    } catch (e) {
      if ((e as Error).message !== 'expired') {
        screen = 'scanning';
        startCamera();
      }
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
    screen = data.over_receive ? 'confirming_over_receive' : 'confirming';
  }

  async function confirm() {
    if (submitting) return;
    const qty = parseInt(confirmQty, 10);
    if (isNaN(qty) || qty <= 0) return;
    submitting = true;
    try {
      const data = await apiJSON<Record<string, unknown>>('POST', '/api/v1/scan/confirm', { qty });
      confirmData = null;
      // Check if order is fully received.
      if (data.order_status === 'received') {
        screen = 'order_complete';
      } else {
        screen = 'scanning';
        startCamera();
      }
    } catch (e) {
      if ((e as Error).message !== 'expired') {
        // keep state for retry
      }
    } finally {
      submitting = false;
    }
  }

  async function skip() {
    apiFetch('POST', '/api/v1/scan/skip').catch(() => {});
    confirmData = null;
    screen = 'scanning';
    startCamera();
  }

  // ── Order list ────────────────────────────────────────────────────────────

  async function loadOrders() {
    ordersLoading = true;
    try {
      const data = await apiJSON<{ orders: ScanOrder[] }>('GET', '/api/v1/scan/orders');
      orders = data.orders;
    } catch {
      orders = [];
    } finally {
      ordersLoading = false;
    }
  }

  async function selectOrder(order: ScanOrder) {
    try {
      await apiJSON('POST', '/api/v1/scan/context', { order_id: order.id });
      contextOrderId = order.id;
      contextOrderRef = order.ref;
      screen = 'scanning';
    } catch {}
  }

  // ── Find Part ─────────────────────────────────────────────────────────────

  function searchParts(q: string) {
    if (partSearchDebounce) clearTimeout(partSearchDebounce);
    partSearchDebounce = setTimeout(async () => {
      if (!q.trim()) { partResults = []; return; }
      partSearchLoading = true;
      try {
        const data = await apiJSON<{ parts: ScanPart[] }>('GET', `/api/v1/scan/parts?q=${encodeURIComponent(q)}`);
        partResults = data.parts;
      } catch {
        partResults = [];
      } finally {
        partSearchLoading = false;
      }
    }, 300);
  }

  $effect(() => {
    if (screen === 'find_part_search') {
      searchParts(partSearchQ);
    }
  });

  function selectPartResult(p: ScanPart) {
    selectedPart = p;
    screen = 'find_part_result';
  }

  // ── Stock Adjustment ──────────────────────────────────────────────────────

  function searchAdjParts(q: string) {
    if (adjSearchDebounce) clearTimeout(adjSearchDebounce);
    adjSearchDebounce = setTimeout(async () => {
      if (!q.trim()) { adjResults = []; return; }
      adjSearchLoading = true;
      try {
        const data = await apiJSON<{ parts: ScanPart[] }>('GET', `/api/v1/scan/parts?q=${encodeURIComponent(q)}`);
        adjResults = data.parts;
      } catch {
        adjResults = [];
      } finally {
        adjSearchLoading = false;
      }
    }, 300);
  }

  $effect(() => {
    if (screen === 'stock_adj_search') {
      searchAdjParts(adjSearchQ);
    }
  });

  function selectAdjPart(p: ScanPart) {
    adjPart = p;
    adjDelta = '-1';
    adjReason = '';
    screen = 'stock_adj_qty';
  }

  async function submitAdj() {
    if (!adjPart?.inventory_entry_id || adjSubmitting) return;
    const delta = parseInt(adjDelta, 10);
    if (isNaN(delta) || delta === 0) return;
    adjSubmitting = true;
    try {
      const data = await apiJSON<{ ok: boolean; new_quantity_on_hand: number }>(
        'POST',
        '/api/v1/scan/stock-adjustment',
        {
          inventory_entry_id: adjPart.inventory_entry_id,
          delta,
          reason: adjReason || undefined,
        },
      );
      adjNewQty = data.new_quantity_on_hand;
      screen = 'stock_adj_done';
    } catch {
      // keep state for retry
    } finally {
      adjSubmitting = false;
    }
  }

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  onMount(() => {
    const params = new URLSearchParams(window.location.search);
    let t = params.get('token') ?? '';
    if (t) {
      localStorage.setItem(TOKEN_KEY, t);
      // Remove token from URL without reload.
      history.replaceState({}, '', window.location.pathname);
    } else {
      t = localStorage.getItem(TOKEN_KEY) ?? '';
    }
    if (!t) {
      screen = 'expired';
      return;
    }
    token = t;
    openPhoneSSE(t);
    startPolling();
  });

  onDestroy(() => {
    stopPolling();
    stopCamera();
    phoneES?.close();
    phoneES = null;
  });

  // ── Helpers ───────────────────────────────────────────────────────────────

  function goToMenu() {
    screen = 'menu';
  }

  function goToScanning() {
    if (contextOrderId) {
      screen = 'scanning';
    } else {
      loadOrders();
      screen = 'order_list';
    }
  }
</script>

<div class="flex h-screen flex-col bg-black text-white overflow-hidden select-none">

  <!-- ── LOADING ──────────────────────────────────────────────────────────── -->
  {#if screen === 'loading'}
    <div class="flex flex-1 flex-col items-center justify-center gap-3">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
      <p class="text-sm text-zinc-400">Connecting…</p>
    </div>

  <!-- ── EXPIRED ──────────────────────────────────────────────────────────── -->
  {:else if screen === 'expired'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-5xl">⏱</div>
      <div>
        <p class="text-lg font-semibold">Session expired</p>
        <p class="mt-2 text-sm text-zinc-400">
          Reconnect by scanning a new QR code on the desktop, or open the phone connection badge.
        </p>
      </div>
    </div>

  <!-- ── MENU ─────────────────────────────────────────────────────────────── -->
  {:else if screen === 'menu'}
    <div class="flex flex-1 flex-col">
      <!-- Header -->
      <div class="px-4 py-4 border-b border-zinc-800">
        <p class="text-lg font-semibold">Scanner</p>
        {#if contextOrderRef}
          <p class="text-xs text-zinc-400 mt-0.5">
            Order context: <span class="text-zinc-200">{contextOrderRef}</span>
          </p>
        {/if}
      </div>

      <!-- Menu items -->
      <div class="flex flex-col gap-2 p-4">
        <button
          onclick={goToScanning}
          class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
        >
          <div class="flex h-10 w-10 items-center justify-center rounded-full bg-blue-500/20 text-blue-400">
            <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M3 9a2 2 0 012-2h.93a2 2 0 001.664-.89l.812-1.22A2 2 0 0110.07 4h3.86a2 2 0 011.664.89l.812 1.22A2 2 0 0018.07 7H19a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2V9z" />
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 13a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </div>
          <div>
            <p class="font-medium">Scan</p>
            <p class="text-xs text-zinc-400">
              {contextOrderRef ? `Receive ${contextOrderRef}` : 'Pick an order to receive'}
            </p>
          </div>
        </button>

        <button
          onclick={() => { partSearchQ = ''; partResults = []; screen = 'find_part_search'; }}
          class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
        >
          <div class="flex h-10 w-10 items-center justify-center rounded-full bg-green-500/20 text-green-400">
            <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </div>
          <div>
            <p class="font-medium">Find Part</p>
            <p class="text-xs text-zinc-400">Search catalog and check stock</p>
          </div>
        </button>

        <button
          onclick={() => { adjSearchQ = ''; adjResults = []; adjPart = null; screen = 'stock_adj_search'; }}
          class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
        >
          <div class="flex h-10 w-10 items-center justify-center rounded-full bg-orange-500/20 text-orange-400">
            <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
            </svg>
          </div>
          <div>
            <p class="font-medium">Stock Adjustment</p>
            <p class="text-xs text-zinc-400">Adjust inventory quantity</p>
          </div>
        </button>
      </div>
    </div>

  <!-- ── ORDER LIST ────────────────────────────────────────────────────────── -->
  {:else if screen === 'order_list'}
    <div class="flex flex-1 flex-col">
      <!-- Header -->
      <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
        <button onclick={goToMenu} class="text-zinc-400 active:text-white">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <p class="font-medium">Select Order</p>
      </div>

      <div class="flex-1 overflow-y-auto p-4 space-y-2">
        {#if ordersLoading}
          <div class="flex justify-center py-8">
            <div class="h-6 w-6 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
          </div>
        {:else if orders.length === 0}
          <div class="py-12 text-center text-zinc-400 text-sm">No receivable orders</div>
        {:else}
          {#each orders as order}
            <button
              onclick={() => selectOrder(order)}
              class="w-full rounded-xl bg-zinc-900 px-4 py-3 text-left active:bg-zinc-800"
            >
              <p class="font-medium">{order.ref}</p>
              <p class="text-xs text-zinc-400 mt-0.5">{order.distributor_name} · {order.item_count} items</p>
            </button>
          {/each}
        {/if}
      </div>
    </div>

  <!-- ── SCANNING ──────────────────────────────────────────────────────────── -->
  {:else if screen === 'scanning'}
    <div class="relative flex-1 overflow-hidden">
      <!-- Back button -->
      <button
        onclick={goToMenu}
        class="absolute top-3 left-3 z-10 flex h-8 w-8 items-center justify-center rounded-full bg-black/50 text-white"
      >
        <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>

      <!-- Context banner -->
      {#if contextOrderRef}
        <div class="absolute top-3 left-0 right-0 z-10 text-center">
          <span class="rounded-full bg-black/60 px-3 py-1 text-xs text-zinc-300">
            {contextOrderRef}
          </span>
        </div>
      {/if}

      <!-- svelte-ignore a11y_media_has_caption -->
      <video bind:this={videoEl} autoplay playsinline muted class="h-full w-full object-cover"></video>

      <!-- Viewfinder overlay -->
      <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
        <div class="h-56 w-56 rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
      </div>
      <p class="absolute bottom-6 left-0 right-0 text-center text-xs text-white/60">
        Point the camera at a bag barcode
      </p>
    </div>

  <!-- ── CONFIRMING ────────────────────────────────────────────────────────── -->
  {:else if screen === 'confirming' || screen === 'confirming_over_receive'}
    <div class="flex flex-1 flex-col justify-end">
      <!-- Back button -->
      <button
        onclick={() => { confirmData = null; screen = 'scanning'; startCamera(); }}
        class="absolute top-3 left-3 flex h-8 w-8 items-center justify-center rounded-full bg-zinc-800 text-white"
      >
        <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>

      <div class="rounded-t-2xl bg-zinc-900 px-5 pt-5 pb-8 space-y-4">
        {#if screen === 'confirming_over_receive'}
          <div class="rounded-lg bg-amber-950 border border-amber-700 px-4 py-3 text-sm text-amber-300">
            <span class="font-semibold">Over-receive:</span>
            Bag has {confirmData?.barcodeQty} but only {(confirmData?.quantityOrdered ?? 0) - (confirmData?.alreadyReceived ?? 0)} remaining.
          </div>
        {/if}

        <div>
          <p class="text-base font-semibold leading-snug">{confirmData?.partName || '—'}</p>
          <p class="mt-0.5 text-sm text-zinc-400">
            {#if confirmData?.mpn}MPN: {confirmData.mpn}{/if}
            {#if confirmData?.mpn && confirmData?.sku} · {/if}
            {#if confirmData?.sku}SKU: {confirmData.sku}{/if}
          </p>
        </div>

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

        <p class="text-sm text-zinc-400">Ordered: {confirmData?.quantityOrdered} · Received: {confirmData?.alreadyReceived}</p>

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

  <!-- ── ORDER COMPLETE ────────────────────────────────────────────────────── -->
  {:else if screen === 'order_complete'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="flex h-16 w-16 items-center justify-center rounded-full bg-green-500/20">
        <svg class="h-8 w-8 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
        </svg>
      </div>
      <div>
        <p class="text-lg font-semibold">Order complete!</p>
        <p class="mt-2 text-sm text-zinc-400">All items have been received.</p>
      </div>
      <button
        onclick={goToMenu}
        class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Back to Menu
      </button>
    </div>

  <!-- ── ERROR NO MATCH ────────────────────────────────────────────────────── -->
  {:else if screen === 'error_no_match'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-4xl text-zinc-400">✕</div>
      <div>
        <p class="text-lg font-semibold">Not in this order</p>
        {#if errorMPN || errorSKU}
          <p class="mt-2 text-sm text-zinc-400">
            {errorMPN ? `MPN: ${errorMPN}` : ''}{errorMPN && errorSKU ? ' · ' : ''}{errorSKU ? `SKU: ${errorSKU}` : ''}
          </p>
        {/if}
      </div>
      <button
        onclick={() => { screen = 'scanning'; startCamera(); }}
        class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Scan Next
      </button>
    </div>

  <!-- ── ERROR PARSE ───────────────────────────────────────────────────────── -->
  {:else if screen === 'error_parse'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="text-4xl text-zinc-400">⚠</div>
      <div>
        <p class="text-lg font-semibold">Unrecognized barcode format</p>
        <p class="mt-2 text-sm text-zinc-400">Only DigiKey and Mouser bag labels are supported.</p>
      </div>
      <button
        onclick={() => { screen = 'scanning'; startCamera(); }}
        class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Scan Next
      </button>
    </div>

  <!-- ── FIND PART SEARCH ──────────────────────────────────────────────────── -->
  {:else if screen === 'find_part_search'}
    <div class="flex flex-1 flex-col">
      <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
        <button onclick={goToMenu} class="text-zinc-400 active:text-white shrink-0">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <input
          type="search"
          placeholder="Search parts…"
          bind:value={partSearchQ}
          oninput={() => searchParts(partSearchQ)}
          autofocus
          class="flex-1 bg-transparent text-white placeholder-zinc-500 focus:outline-none text-sm"
        />
        {#if partSearchLoading}
          <div class="h-4 w-4 animate-spin rounded-full border border-zinc-600 border-t-white shrink-0"></div>
        {/if}
      </div>

      <div class="flex-1 overflow-y-auto divide-y divide-zinc-800">
        {#if partResults.length === 0 && partSearchQ.trim()}
          <div class="py-12 text-center text-zinc-400 text-sm">No parts found</div>
        {:else}
          {#each partResults as part}
            <button
              onclick={() => selectPartResult(part)}
              class="w-full px-4 py-3 text-left active:bg-zinc-900"
            >
              <p class="font-medium text-sm">{part.mpn ?? '—'}</p>
              {#if part.description}
                <p class="text-xs text-zinc-400 mt-0.5 line-clamp-1">{part.description}</p>
              {/if}
              {#if part.quantity_on_hand !== null}
                <p class="text-xs text-zinc-500 mt-0.5">Stock: {part.quantity_on_hand}</p>
              {/if}
            </button>
          {/each}
        {/if}
      </div>
    </div>

  <!-- ── FIND PART RESULT ──────────────────────────────────────────────────── -->
  {:else if screen === 'find_part_result' && selectedPart}
    <div class="flex flex-1 flex-col">
      <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
        <button onclick={() => screen = 'find_part_search'} class="text-zinc-400 active:text-white">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <p class="font-medium">Part Details</p>
      </div>

      <div class="flex-1 overflow-y-auto p-4 space-y-4">
        <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
          <p class="text-lg font-semibold">{selectedPart.mpn ?? '—'}</p>
          {#if selectedPart.description}
            <p class="text-sm text-zinc-400">{selectedPart.description}</p>
          {/if}
        </div>

        <div class="rounded-xl bg-zinc-900 p-4 space-y-2">
          <div class="flex justify-between text-sm">
            <span class="text-zinc-400">Stock</span>
            <span class="font-medium">{selectedPart.quantity_on_hand ?? 'Not tracked'}</span>
          </div>
          {#if selectedPart.storage_location}
            <div class="flex justify-between text-sm">
              <span class="text-zinc-400">Location</span>
              <span class="font-medium">{selectedPart.storage_location}</span>
            </div>
          {/if}
        </div>

        {#if selectedPart.inventory_entry_id}
          <button
            onclick={() => {
              adjPart = selectedPart;
              adjDelta = '-1';
              adjReason = '';
              screen = 'stock_adj_qty';
            }}
            class="w-full rounded-xl bg-orange-500/20 border border-orange-500/30 py-3 text-orange-300 font-medium active:bg-orange-500/30"
          >
            Adjust Stock
          </button>
        {:else}
          <p class="text-center text-xs text-zinc-500">Part not tracked in inventory</p>
        {/if}
      </div>
    </div>

  <!-- ── STOCK ADJ SEARCH ──────────────────────────────────────────────────── -->
  {:else if screen === 'stock_adj_search'}
    <div class="flex flex-1 flex-col">
      <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
        <button onclick={goToMenu} class="text-zinc-400 active:text-white shrink-0">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <input
          type="search"
          placeholder="Search parts to adjust…"
          bind:value={adjSearchQ}
          oninput={() => searchAdjParts(adjSearchQ)}
          autofocus
          class="flex-1 bg-transparent text-white placeholder-zinc-500 focus:outline-none text-sm"
        />
        {#if adjSearchLoading}
          <div class="h-4 w-4 animate-spin rounded-full border border-zinc-600 border-t-white shrink-0"></div>
        {/if}
      </div>

      <div class="flex-1 overflow-y-auto divide-y divide-zinc-800">
        {#if adjResults.length === 0 && adjSearchQ.trim()}
          <div class="py-12 text-center text-zinc-400 text-sm">No parts found</div>
        {:else}
          {#each adjResults as part}
            <button
              onclick={() => part.inventory_entry_id ? selectAdjPart(part) : null}
              class="w-full px-4 py-3 text-left {part.inventory_entry_id ? 'active:bg-zinc-900' : 'opacity-50'}"
              disabled={!part.inventory_entry_id}
            >
              <p class="font-medium text-sm">{part.mpn ?? '—'}</p>
              {#if part.description}
                <p class="text-xs text-zinc-400 mt-0.5 line-clamp-1">{part.description}</p>
              {/if}
              <p class="text-xs text-zinc-500 mt-0.5">
                {part.inventory_entry_id ? `Stock: ${part.quantity_on_hand ?? 0}` : 'Not tracked'}
              </p>
            </button>
          {/each}
        {/if}
      </div>
    </div>

  <!-- ── STOCK ADJ QTY ─────────────────────────────────────────────────────── -->
  {:else if screen === 'stock_adj_qty' && adjPart}
    <div class="flex flex-1 flex-col">
      <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
        <button onclick={() => screen = 'stock_adj_search'} class="text-zinc-400 active:text-white">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <p class="font-medium">Adjust Stock</p>
      </div>

      <div class="flex-1 overflow-y-auto p-4 space-y-4">
        <div class="rounded-xl bg-zinc-900 p-4">
          <p class="font-medium">{adjPart.mpn ?? '—'}</p>
          <p class="text-xs text-zinc-400 mt-0.5">Current stock: {adjPart.quantity_on_hand ?? 0}</p>
        </div>

        <div class="space-y-3">
          <div>
            <p class="mb-2 text-xs font-medium text-zinc-400 uppercase tracking-wide">Delta (negative = remove)</p>
            <div class="flex items-center gap-3">
              <button
                onclick={() => { const v = parseInt(adjDelta, 10) || 0; adjDelta = String(v - 1); }}
                class="flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-700 text-lg active:bg-zinc-800"
              >−</button>
              <input
                type="number"
                bind:value={adjDelta}
                inputmode="numeric"
                class="flex-1 rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-lg font-mono text-white text-center focus:outline-none focus:ring-2 focus:ring-white/30"
              />
              <button
                onclick={() => { const v = parseInt(adjDelta, 10) || 0; adjDelta = String(v + 1); }}
                class="flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-700 text-lg active:bg-zinc-800"
              >+</button>
            </div>
          </div>

          <div>
            <p class="mb-1 text-xs font-medium text-zinc-400 uppercase tracking-wide">Reason (optional)</p>
            <input
              type="text"
              bind:value={adjReason}
              placeholder="e.g. Counted, damaged, etc."
              class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-white/30"
            />
          </div>
        </div>

        <button
          onclick={submitAdj}
          disabled={adjSubmitting || adjDelta === '0' || adjDelta === ''}
          class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200 disabled:opacity-50"
        >
          {adjSubmitting ? 'Saving…' : 'Confirm'}
        </button>
      </div>
    </div>

  <!-- ── STOCK ADJ DONE ────────────────────────────────────────────────────── -->
  {:else if screen === 'stock_adj_done'}
    <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
      <div class="flex h-16 w-16 items-center justify-center rounded-full bg-green-500/20">
        <svg class="h-8 w-8 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
        </svg>
      </div>
      <div>
        <p class="text-lg font-semibold">Adjustment saved</p>
        {#if adjNewQty !== null}
          <p class="mt-2 text-sm text-zinc-400">New stock: <span class="text-white font-medium">{adjNewQty}</span></p>
        {/if}
      </div>
      <button
        onclick={goToMenu}
        class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
      >
        Back to Menu
      </button>
    </div>

  {/if}

</div>

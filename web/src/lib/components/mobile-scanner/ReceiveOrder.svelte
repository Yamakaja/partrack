<script lang="ts">
  import { untrack } from 'svelte';
  import { createScanner } from './scanner.svelte.js';
  import type { ScanOrder } from './types.js';

  let {
    token,
    contextOrderId: initialContextOrderId,  // snapshot — used only to seed initial screen state
    contextOrderRef,
    onBack,
    onContextChange,
    onExpired,
  }: {
    token: string;
    contextOrderId: string | null;
    contextOrderRef: string | null;
    onBack: () => void;
    onContextChange: (orderId: string, orderRef: string) => void;
    onExpired: () => void;
  } = $props();

  // ── Internal screen state ─────────────────────────────────────────────────

  type Screen =
    | 'order_list'
    | 'scanning'
    | 'confirming'
    | 'confirming_over_receive'
    | 'order_complete'
    | 'error_no_match'
    | 'error_parse';

  let screen = $state<Screen>(untrack(() => initialContextOrderId ? 'scanning' : 'order_list'));
  let currentOrderId = $state<string>(untrack(() => initialContextOrderId ?? ''));

  // ── Order list ────────────────────────────────────────────────────────────

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
  let errorMPN = $state('');
  let errorSKU = $state('');

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

  function selectOrder(order: ScanOrder) {
    currentOrderId = order.id;
    onContextChange(order.id, order.ref);
    screen = 'scanning';
  }

  // ── Scan handling ─────────────────────────────────────────────────────────

  async function handleScanResult(raw: string) {
    const data = await apiJSON<Record<string, unknown>>('POST', '/api/v1/scan/scans', { order_id: currentOrderId, barcode: raw });

    if (data.status === 'matched') {
      applyPendingScan(data);
    } else if (data.status === 'no_match') {
      screen = 'error_no_match';
      errorMPN = (data.parsed_mpn as string) || '';
      errorSKU = (data.parsed_sku as string) || '';
    } else {
      screen = 'error_parse';
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
      screen = data.order_status === 'received' ? 'order_complete' : 'scanning';
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
  }

  // ── Init ──────────────────────────────────────────────────────────────────

  untrack(() => { if (!initialContextOrderId) loadOrders(); });
</script>

<!-- ── ORDER LIST ──────────────────────────────────────────────────────────── -->
{#if screen === 'order_list'}
  <div class="flex flex-1 flex-col">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
      <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white">
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
    <button
      onclick={onBack}
      aria-label="Back"
      class="absolute top-3 left-3 z-10 flex h-8 w-8 items-center justify-center rounded-full bg-black/50 text-white"
    >
      <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>

    {#if contextOrderRef}
      <div class="absolute top-3 left-0 right-0 z-10 text-center">
        <span class="rounded-full bg-black/60 px-3 py-1 text-xs text-zinc-300">
          {contextOrderRef}
        </span>
      </div>
    {/if}

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
          onclick={() => scanner.triggerScan(handleScanResult)}
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

<!-- ── CONFIRMING ────────────────────────────────────────────────────────── -->
{:else if screen === 'confirming' || screen === 'confirming_over_receive'}
  <div class="flex flex-1 flex-col justify-end">
    <button
      onclick={() => { confirmData = null; screen = 'scanning'; }}
      aria-label="Back"
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
      onclick={onBack}
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
      onclick={() => { screen = 'scanning'; }}
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
      onclick={() => { screen = 'scanning'; }}
      class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
    >
      Scan Next
    </button>
  </div>
{/if}

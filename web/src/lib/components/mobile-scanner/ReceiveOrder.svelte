<script lang="ts">
  import { untrack } from 'svelte';
  import ScannerCamera from './ScannerCamera.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
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

  let screen = $state<Screen>('order_list');
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
    onContextChange(order.id, order.title);
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

  untrack(() => { loadOrders(); });
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
          {@const isContext = order.id === currentOrderId}
          {@const receivedItems = order.items_fully_received}
          {@const totalItems = order.item_count}
          {@const allReceived = receivedItems === totalItems}
          <button
            onclick={() => selectOrder(order)}
            class="w-full rounded-xl px-4 py-3 text-left active:bg-zinc-800 {isContext ? 'bg-zinc-700 ring-1 ring-white/20' : 'bg-zinc-900'}"
          >
            <!-- Row 1: title + creator badge -->
            <div class="flex items-center justify-between gap-2">
              <div class="flex items-center gap-1.5 min-w-0">
                {#if order.title_is_fallback}
                  <span class="shrink-0 text-xs text-zinc-600 font-medium">ID</span>
                {/if}
                <p class="font-medium font-mono text-sm truncate {order.title_is_fallback ? 'text-zinc-500' : 'text-white'}">{order.title}</p>
              </div>
              {#if order.created_by_me}
                <span class="shrink-0 text-xs bg-blue-500/20 text-blue-300 px-2 py-0.5 rounded-full">You</span>
              {:else}
                <span class="shrink-0 text-xs text-zinc-500 truncate max-w-[8rem]">{order.created_by_name}</span>
              {/if}
            </div>

            <!-- Row 1b: external ref (when present) -->
            {#if order.external_ref}
              <div class="flex items-center gap-1.5 mt-0.5">
                <span class="text-xs text-zinc-600 font-medium">Ext</span>
                <span class="text-xs font-mono text-zinc-400 truncate">{order.external_ref}</span>
              </div>
            {/if}

            <!-- Row 2: distributor + date -->
            <div class="flex items-center justify-between mt-0.5">
              <p class="text-xs text-zinc-400">{order.distributor_name}</p>
              <p class="text-xs text-zinc-500">{new Date(order.created_at).toLocaleDateString()}</p>
            </div>

            <!-- Row 3: receipt progress + value -->
            <div class="flex items-center justify-between mt-1.5">
              <div class="flex items-center gap-1.5">
                <span class="text-xs {allReceived ? 'text-green-400' : 'text-zinc-400'}">
                  {receivedItems}/{totalItems} items
                </span>
              </div>
              {#if order.total_value}
                <PriceDisplay value={order.total_value} decimals={2} class="text-xs text-zinc-400" />
              {/if}
            </div>
          </button>
        {/each}
      {/if}
    </div>
  </div>

<!-- ── SCANNING ──────────────────────────────────────────────────────────── -->
{:else if screen === 'scanning'}
  <ScannerCamera
    onBack={() => { screen = 'order_list'; }}
    onScan={handleScanResult}
    label={contextOrderRef}
  />

<!-- ── CONFIRMING ────────────────────────────────────────────────────────── -->
{:else if screen === 'confirming' || screen === 'confirming_over_receive'}
  <div class="relative flex flex-1 flex-col justify-end">
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

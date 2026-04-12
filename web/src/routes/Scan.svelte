<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import type { NavEntry, ScanPart } from '$lib/components/mobile-scanner/types.js';
  import ScanMenu from '$lib/components/mobile-scanner/ScanMenu.svelte';
  import ReceiveOrder from '$lib/components/mobile-scanner/ReceiveOrder.svelte';
  import FindPart from '$lib/components/mobile-scanner/FindPart.svelte';
  import PartDetail from '$lib/components/mobile-scanner/PartDetail.svelte';
  import StockAdjustment from '$lib/components/mobile-scanner/StockAdjustment.svelte';
  import BagScan from '$lib/components/mobile-scanner/BagScan.svelte';
  import DistributorSearch from '$lib/components/mobile-scanner/DistributorSearch.svelte';

  // ── Auth ──────────────────────────────────────────────────────────────────

  const TOKEN_KEY = 'scan_token';
  let token = $state('');

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

  function handleExpired() {
    localStorage.removeItem(TOKEN_KEY);
    token = '';
    stopPolling();
    phoneES?.close();
    phoneES = null;
    topLevel = 'expired';
  }

  // ── Top-level state (outside the nav stack) ───────────────────────────────

  type TopLevel = 'loading' | 'expired';
  let topLevel = $state<TopLevel | null>('loading');

  // ── Navigation stack ──────────────────────────────────────────────────────

  let navStack = $state<NavEntry[]>([{ screen: 'menu' }]);
  let current = $derived(navStack[navStack.length - 1]);

  function goTo(entry: NavEntry) {
    navStack = [...navStack, entry];
  }

  function goBack() {
    if (navStack.length > 1) navStack = navStack.slice(0, -1);
  }

  function goToMenu() {
    navStack = [{ screen: 'menu' }];
    contextOrderId = null;
    contextOrderRef = null;
  }

  // ── Session state ─────────────────────────────────────────────────────────

  let contextOrderId = $state<string | null>(null);
  let contextOrderRef = $state<string | null>(null);

  // ── BagScan part-found handler ────────────────────────────────────────────

  function handlePartFound(part: ScanPart, bagQty: number, newEntry: boolean) {
    if (newEntry) {
      goTo({ screen: 'stock_adjustment', part, mode: 'set', initialValue: bagQty });
    } else {
      goTo({ screen: 'part_detail', part });
    }
  }

  // ── Phone SSE ─────────────────────────────────────────────────────────────

  let phoneES: EventSource | null = null;

  function openPhoneSSE(t: string) {
    phoneES?.close();
    phoneES = new EventSource(`/api/v1/scan/stream?token=${encodeURIComponent(t)}`);
    phoneES.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as Record<string, unknown>;
        if (msg.type === 'scan_context_changed') {
          contextOrderId = (msg.context_order_id as string) ?? null;
          contextOrderRef = (msg.context_order_ref as string) ?? null;
          if (contextOrderId && current.screen === 'menu') {
            goTo({ screen: 'receive_order' });
          }
        }
      } catch {}
    };
  }

  // ── Polling ───────────────────────────────────────────────────────────────

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
      await res.json();
      if (topLevel === 'loading') {
        topLevel = null;
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

  // ── Lifecycle ─────────────────────────────────────────────────────────────

  onMount(() => {
    // Disable pinch-to-zoom on the scanner page.
    // iOS Safari ignores user-scalable=no since iOS 10, so we also block
    // multi-touch move events which is what triggers the zoom gesture.
    const viewport = document.querySelector('meta[name="viewport"]');
    const originalContent = viewport?.getAttribute('content') ?? '';
    viewport?.setAttribute('content', 'width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no');

    // Block pinch-zoom-in but allow pinch-zoom-out so the user can always
    // recover if the page somehow ends up zoomed in.
    let lastPinchDistance = 0;
    function pinchDistance(e: TouchEvent) {
      const dx = e.touches[0].clientX - e.touches[1].clientX;
      const dy = e.touches[0].clientY - e.touches[1].clientY;
      return Math.sqrt(dx * dx + dy * dy);
    }
    function onTouchStart(e: TouchEvent) {
      if (e.touches.length === 2) lastPinchDistance = pinchDistance(e);
    }
    function onTouchMove(e: TouchEvent) {
      if (e.touches.length === 2) {
        const d = pinchDistance(e);
        if (d > lastPinchDistance) e.preventDefault(); // spreading = zoom-in: block
        lastPinchDistance = d;
      }
    }
    document.addEventListener('touchstart', onTouchStart, { passive: true });
    document.addEventListener('touchmove', onTouchMove, { passive: false });

    const params = new URLSearchParams(window.location.search);
    let t = params.get('token') ?? '';
    if (t) {
      localStorage.setItem(TOKEN_KEY, t);
      history.replaceState({}, '', window.location.pathname);
    } else {
      t = localStorage.getItem(TOKEN_KEY) ?? '';
    }
    if (!t) {
      topLevel = 'expired';
      return;
    }
    token = t;
    openPhoneSSE(t);
    startPolling();

    return () => {
      viewport?.setAttribute('content', originalContent);
      document.removeEventListener('touchstart', onTouchStart);
      document.removeEventListener('touchmove', onTouchMove);
    };
  });

  onDestroy(() => {
    stopPolling();
    phoneES?.close();
    phoneES = null;
  });
</script>

<div class="flex h-dvh flex-col bg-black text-white overflow-hidden select-none touch-manipulation">

  <!-- ── LOADING ──────────────────────────────────────────────────────────── -->
  {#if topLevel === 'loading'}
    <div class="flex flex-1 flex-col items-center justify-center gap-3">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
      <p class="text-sm text-zinc-400">Connecting…</p>
    </div>

  <!-- ── EXPIRED ──────────────────────────────────────────────────────────── -->
  {:else if topLevel === 'expired'}
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
  {:else if current.screen === 'menu'}
    <ScanMenu
      {contextOrderRef}
      onReceiveOrder={() => goTo({ screen: 'receive_order' })}
      onBagScan={() => goTo({ screen: 'bag_scan' })}
      onFindPart={() => goTo({ screen: 'find_part', initialQuery: '' })}
    />

  <!-- ── RECEIVE ORDER ─────────────────────────────────────────────────────── -->
  {:else if current.screen === 'receive_order'}
    <ReceiveOrder
      {token}
      {contextOrderId}
      {contextOrderRef}
      onBack={goBack}
      onContextChange={(id, ref) => { contextOrderId = id; contextOrderRef = ref; }}
      onExpired={handleExpired}
    />

  <!-- ── BAG SCAN ──────────────────────────────────────────────────────────── -->
  {:else if current.screen === 'bag_scan'}
    <BagScan
      {token}
      onBack={goBack}
      onPartFound={handlePartFound}
      onPartImported={(part, qty) => goTo({
        screen: 'part_detail', part,
        initialAdjust: { value: qty, mode: 'set', reason: 'Initial Stock' },
      })}
      onSearchDistributor={(distId, distName, q, qty) => goTo({
        screen: 'distributor_search',
        distributorId: distId, distributorName: distName,
        initialQuery: q, parsedQty: qty,
      })}
      onSearchCatalog={(q) => goTo({ screen: 'find_part', initialQuery: q })}
      onExpired={handleExpired}
    />

  <!-- ── DISTRIBUTOR SEARCH ────────────────────────────────────────────────── -->
  {:else if current.screen === 'distributor_search'}
    <DistributorSearch
      {token}
      distributorId={current.distributorId}
      distributorName={current.distributorName}
      initialQuery={current.initialQuery}
      parsedQty={current.parsedQty}
      onBack={goBack}
      onImportDone={(part, qty) => goTo({
        screen: 'part_detail', part,
        initialAdjust: { value: qty, mode: 'set', reason: 'Initial Stock' },
      })}
      onExpired={handleExpired}
    />

  <!-- ── FIND PART ─────────────────────────────────────────────────────────── -->
  {:else if current.screen === 'find_part'}
    <FindPart
      {token}
      initialQuery={current.initialQuery}
      onBack={goBack}
      onSelectPart={(part) => goTo({ screen: 'part_detail', part })}
      onExpired={handleExpired}
    />

  <!-- ── PART DETAIL ───────────────────────────────────────────────────────── -->
  {:else if current.screen === 'part_detail'}
    <PartDetail
      {token}
      part={current.part}
      initialAdjust={current.initialAdjust}
      onBack={goBack}
      onAdjustStock={(part, adjust) => goTo({
        screen: 'stock_adjustment', part,
        mode: adjust?.mode ?? 'adjust',
        initialValue: adjust?.value,
        initialReason: adjust?.reason,
      })}
    />

  <!-- ── STOCK ADJUSTMENT ──────────────────────────────────────────────────── -->
  {:else if current.screen === 'stock_adjustment'}
    <StockAdjustment
      {token}
      part={current.part}
      mode={current.mode}
      initialValue={current.initialValue}
      initialReason={current.initialReason}
      onBack={goBack}
      onDone={goToMenu}
      onExpired={handleExpired}
    />
  {/if}

</div>

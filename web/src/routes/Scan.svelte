<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Toaster from '$lib/components/common/Toaster.svelte';
  import type { NavEntry, ScanPart, PartLocation } from '$lib/components/mobile-scanner/types.js';
  import type { ScanHistoryEntry } from '$lib/stores/scannerHistory.svelte.js';
  import { checkSession, listScanStorageLocations, ScanExpiredError, pairScanSession, createStreamTicket } from '$lib/api/scanner.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import ScanMenu from '$lib/components/mobile-scanner/ScanMenu.svelte';
  import ReceiveOrder from '$lib/components/mobile-scanner/ReceiveOrder.svelte';
  import FindPart from '$lib/components/mobile-scanner/FindPart.svelte';
  import PartDetail from '$lib/components/mobile-scanner/PartDetail.svelte';
  import StockAdjustment from '$lib/components/mobile-scanner/StockAdjustment.svelte';
  import BagScan from '$lib/components/mobile-scanner/BagScan.svelte';
  import ListingPicker from '$lib/components/mobile-scanner/ListingPicker.svelte';
  import LocationInventory from '$lib/components/mobile-scanner/LocationInventory.svelte';
  import StorageAssignment from '$lib/components/mobile-scanner/StorageAssignment.svelte';
  import BuildKitting from '$lib/components/mobile-scanner/BuildKitting.svelte';

  // ── Auth ──────────────────────────────────────────────────────────────────

  const TOKEN_KEY = 'scan_token';
  let token = $state('');

  // ── Storage locations (fetched once, passed to all child screens) ─────────

  let locations = $state<StorageLocation[]>([]);

  async function fetchLocations(t: string) {
    try {
      const res = await listScanStorageLocations(t);
      locations = res.locations;
    } catch {
      // non-fatal — components degrade gracefully with empty list
    }
  }

  function handleExpired() {
    localStorage.removeItem(TOKEN_KEY);
    token = '';
    stopPolling();
    sseStopped = true;
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
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

  function goReplace(entry: NavEntry) {
    navStack = [...navStack.slice(0, -1), entry];
  }

  function goBack() {
    if (navStack.length > 1) navStack = navStack.slice(0, -1);
  }

  function goBack2() {
    navStack = navStack.length > 2 ? navStack.slice(0, -2) : [{ screen: 'menu' }];
  }


  // ── Session state ─────────────────────────────────────────────────────────

  let contextOrderId = $state<string | null>(null);
  let contextOrderRef = $state<string | null>(null);

  // ── Pending context (pushed while the phone is off the menu) ───────────────
  // A desktop "Send to Scanner" push (kitting / order) that arrives while the
  // operator is mid-task must not be silently dropped. We stash it and surface a
  // tappable banner so the operator can switch when ready.
  type PendingContext =
    | { kind: 'kitting'; kittingSessionId: string; buildRef: string }
    | { kind: 'order'; orderId: string; orderRef: string };
  let pendingContext = $state<PendingContext | null>(null);

  function applyContext(ctx: PendingContext) {
    pendingContext = null;
    navStack = [{ screen: 'menu' }];
    if (ctx.kind === 'kitting') {
      goTo({ screen: 'build_kitting', kittingSessionId: ctx.kittingSessionId, buildRef: ctx.buildRef });
    } else {
      contextOrderId = ctx.orderId;
      contextOrderRef = ctx.orderRef;
      goTo({ screen: 'receive_order' });
    }
  }

  // Route an incoming context: navigate immediately when idle on the menu,
  // otherwise raise the banner so the operator isn't yanked out of a task.
  function receiveContext(ctx: PendingContext) {
    if (
      ctx.kind === 'kitting' &&
      current.screen === 'build_kitting' &&
      current.kittingSessionId === ctx.kittingSessionId
    ) {
      return; // already in this kitting session
    }
    if (current.screen === 'menu') applyContext(ctx);
    else pendingContext = ctx;
  }

  // ── History part-select handler ───────────────────────────────────────────

  function handleHistoryPartSelect(entry: ScanHistoryEntry) {
    const part: ScanPart = {
      id: entry.partId,
      mpn: entry.mpn,
      description: entry.description,
      manufacturer: entry.manufacturer,
      inventory_entry_id: entry.inventoryEntryId,
      quantity_on_hand: entry.quantityOnHand,
      storage_location: entry.storageLocation,
    };
    goTo({ screen: 'part_detail', part });
  }

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
  let sseStopped = false;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectAttempts = 0;

  // openPhoneSSE mints a fresh single-use stream ticket and opens the SSE stream
  // with it (the durable token is never placed in the URL). Because the ticket is
  // single-use, native EventSource auto-reconnect would fail with a spent ticket,
  // so on any error we close the stream and reconnect with a newly minted ticket.
  async function openPhoneSSE() {
    phoneES?.close();
    phoneES = null;
    if (!token || sseStopped) return;

    let ticket: string;
    try {
      ticket = (await createStreamTicket(token)).ticket;
    } catch (e) {
      if (e instanceof ScanExpiredError) { handleExpired(); return; }
      scheduleReconnect();
      return;
    }

    const es = new EventSource(`/api/v1/scan/stream?ticket=${encodeURIComponent(ticket)}`);
    phoneES = es;
    es.onopen = () => { reconnectAttempts = 0; };
    es.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as Record<string, unknown>;
        if (msg.type === 'scan_context_changed') {
          if (msg.context_type === 'kitting') {
            const ksId = msg.context_kitting_session_id as string | undefined;
            const ref = (msg.context_build_ref as string) ?? '';
            if (ksId) receiveContext({ kind: 'kitting', kittingSessionId: ksId, buildRef: ref });
          } else {
            const orderId = (msg.context_order_id as string) ?? null;
            const orderRef = (msg.context_order_ref as string) ?? null;
            // Keep the stored order context fresh regardless of current screen.
            contextOrderId = orderId;
            contextOrderRef = orderRef;
            if (orderId) receiveContext({ kind: 'order', orderId, orderRef: orderRef ?? '' });
          }
        }
      } catch {}
    };
    es.onerror = () => {
      es.close();
      if (phoneES === es) phoneES = null;
      scheduleReconnect(); // mint a fresh ticket and reconnect
    };
  }

  // Exponential backoff with jitter, capped at 30s. Reset to 0 on a successful
  // open (es.onopen) or when the tab is brought back to the foreground.
  function scheduleReconnect() {
    if (sseStopped || reconnectTimer) return;
    const base = Math.min(30000, 1000 * 2 ** reconnectAttempts);
    const delay = base / 2 + Math.random() * (base / 2); // 0.5×–1× base
    reconnectAttempts = Math.min(reconnectAttempts + 1, 6);
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      openPhoneSSE();
    }, delay);
  }

  // When the tab returns to the foreground (iOS often freezes/kills the SSE
  // socket and timers while backgrounded), recover promptly: cancel any pending
  // backoff, reset it, reconnect if the stream is gone, and poll once so context
  // pushes missed while asleep are picked up.
  function handleVisibility() {
    if (document.visibilityState !== 'visible' || sseStopped || !token) return;
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
    reconnectAttempts = 0;
    if (!phoneES || phoneES.readyState === EventSource.CLOSED) void openPhoneSSE();
    void pollSession();
  }

  // ── Polling ───────────────────────────────────────────────────────────────

  let pollInterval: ReturnType<typeof setInterval> | null = null;

  async function pollSession() {
    if (!token) return;
    try {
      const s = await checkSession(token);
      if (topLevel === 'loading') topLevel = null;
      if (s.active_kitting_session_id && current.screen === 'menu') {
        goTo({ screen: 'build_kitting', kittingSessionId: s.active_kitting_session_id, buildRef: s.active_kitting_build_ref ?? '' });
      }
    } catch (e) {
      if (e instanceof ScanExpiredError) { handleExpired(); return; }
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
    document.addEventListener('visibilitychange', handleVisibility);

    // Pairing ticket arrives in the URL fragment (#pt=…) so it is never sent to
    // the server / proxy logs. Exchange it once for the durable token.
    let pairingTicket = '';
    if (window.location.hash.startsWith('#pt=')) {
      pairingTicket = decodeURIComponent(window.location.hash.slice(4));
      history.replaceState({}, '', window.location.pathname);
    }

    void (async () => {
      let t = localStorage.getItem(TOKEN_KEY) ?? '';
      if (pairingTicket) {
        try {
          t = (await pairScanSession(pairingTicket)).token;
          localStorage.setItem(TOKEN_KEY, t);
        } catch {
          // fall back to any stored token below
        }
      }
      if (!t) {
        topLevel = 'expired';
        return;
      }
      token = t;
      sseStopped = false;
      await openPhoneSSE();
      startPolling();
      fetchLocations(t);
    })();

    return () => {
      viewport?.setAttribute('content', originalContent);
      document.removeEventListener('touchstart', onTouchStart);
      document.removeEventListener('touchmove', onTouchMove);
      document.removeEventListener('visibilitychange', handleVisibility);
    };
  });

  onDestroy(() => {
    stopPolling();
    sseStopped = true;
    if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
    phoneES?.close();
    phoneES = null;
  });
</script>

<div class="flex h-dvh flex-col bg-black text-white overflow-hidden select-none touch-manipulation">

  <!-- ── PENDING CONTEXT BANNER (pushed while off the menu) ─────────────────── -->
  {#if pendingContext && topLevel === null && current.screen !== 'menu'}
    <div class="fixed inset-x-0 top-0 z-[60] p-2">
      <div class="flex items-center gap-3 rounded-lg border border-white/20 bg-zinc-900/95 px-3 py-2 shadow-lg backdrop-blur">
        <button
          type="button"
          class="flex min-w-0 flex-1 items-center gap-3 text-left active:opacity-70"
          onclick={() => pendingContext && applyContext(pendingContext)}
        >
          <span class="text-lg">{pendingContext.kind === 'kitting' ? '🧰' : '📦'}</span>
          <span class="min-w-0">
            <span class="block text-xs text-zinc-400">
              {pendingContext.kind === 'kitting' ? 'Kitting ready — tap to open' : 'Order ready — tap to receive'}
            </span>
            <span class="block truncate text-sm font-medium">
              {pendingContext.kind === 'kitting'
                ? (pendingContext.buildRef || 'Build')
                : (pendingContext.orderRef || 'Order')}
            </span>
          </span>
        </button>
        <button
          type="button"
          aria-label="Dismiss"
          class="shrink-0 rounded p-1 text-zinc-400 active:text-white"
          onclick={() => (pendingContext = null)}
        >✕</button>
      </div>
    </div>
  {/if}

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
      onPartSelect={handleHistoryPartSelect}
      onStorageAssignment={() => goTo({ screen: 'storage_assignment' })}
    />

  <!-- ── RECEIVE ORDER ─────────────────────────────────────────────────────── -->
  {:else if current.screen === 'receive_order'}
    <ReceiveOrder
      {token}
      {locations}
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
      onStartImport={(ctx) => goTo({ screen: 'listing_picker', context: ctx })}
      onLocationScanned={(loc: PartLocation) => goTo({ screen: 'location_inventory', locationId: loc.id, coordinate: loc.coordinate })}
      onExpired={handleExpired}
    />

  <!-- ── LISTING PICKER ────────────────────────────────────────────────────── -->
  {:else if current.screen === 'listing_picker'}
    <ListingPicker
      {token}
      context={current.context}
      onBack={goBack}
      onImportDone={(part, qty) => goReplace({
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
      {locations}
      part={current.part}
      initialAdjust={current.initialAdjust}
      onBack={goBack}
      onAdjustStock={(part, adjust) => goTo({
        screen: 'stock_adjustment', part,
        mode: adjust?.mode ?? 'adjust',
        initialValue: adjust?.value,
        initialReason: adjust?.reason,
      })}
      onExpired={handleExpired}
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
      onDone={goBack2}
      onExpired={handleExpired}
    />

  <!-- ── LOCATION INVENTORY ────────────────────────────────────────────────── -->
  {:else if current.screen === 'location_inventory'}
    <LocationInventory
      {token}
      {locations}
      locationId={current.locationId}
      coordinate={current.coordinate}
      onBack={goBack}
      onPartSelect={(part) => goTo({ screen: 'part_detail', part })}
      onExpired={handleExpired}
    />

  <!-- ── STORAGE ASSIGNMENT ───────────────────────────────────────────────── -->
  {:else if current.screen === 'storage_assignment'}
    <StorageAssignment
      {token}
      {locations}
      onBack={goBack}
      onExpired={handleExpired}
    />

  <!-- ── BUILD KITTING ─────────────────────────────────────────────────────── -->
  {:else if current.screen === 'build_kitting'}
    <BuildKitting
      {token}
      {locations}
      kittingSessionId={current.kittingSessionId}
      buildRef={current.buildRef}
      onBack={goBack}
      onExpired={handleExpired}
    />
  {/if}

</div>

<Toaster position="top-right" />

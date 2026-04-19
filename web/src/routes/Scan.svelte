<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Toaster from '$lib/components/common/Toaster.svelte';
  import type { NavEntry, ScanPart, PartLocation } from '$lib/components/mobile-scanner/types.js';
  import type { ScanHistoryEntry } from '$lib/stores/scannerHistory.svelte.js';
  import { checkSession, listScanStorageLocations, ScanExpiredError } from '$lib/api/scanner.js';
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

  function openPhoneSSE(t: string) {
    phoneES?.close();
    phoneES = new EventSource(`/api/v1/scan/stream?token=${encodeURIComponent(t)}`);
    phoneES.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as Record<string, unknown>;
        if (msg.type === 'scan_context_changed') {
          if (msg.context_type === 'kitting') {
            const ksId = msg.context_kitting_session_id as string | undefined;
            const ref = msg.context_build_ref as string | undefined;
            if (ksId && current.screen === 'menu') {
              goTo({ screen: 'build_kitting', kittingSessionId: ksId, buildRef: ref ?? '' });
            }
          } else {
            contextOrderId = (msg.context_order_id as string) ?? null;
            contextOrderRef = (msg.context_order_ref as string) ?? null;
            if (contextOrderId && current.screen === 'menu') {
              goTo({ screen: 'receive_order' });
            }
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
    fetchLocations(t);

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

<script lang="ts">
  import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
  import type { PartLocation } from './types.js';
  import { parseStorageLocationQR } from '$lib/utils/ipc2d.js';
  import { fullLocationLabel } from './locationUtils.js';
  import { createScanner } from './scanner.svelte.js';
  import CameraSelector from './CameraSelector.svelte';
  import MobileGridVisualiser from './MobileGridVisualiser.svelte';
  import StorageTree from '$lib/components/storage/StorageTree.svelte';

  interface Props {
    locations: StorageLocation[];
    value: PartLocation | null;
    onSelect: (v: PartLocation | null) => void;
    onClose: () => void;
    /** If set, skip the tree and open directly at this location's cell selection screen. */
    initialLocationId?: string;
    /** When false, hides the "Use location only (no cell)" button. Defaults to true. */
    allowLocationOnly?: boolean;
    /** When true, tapping a cell immediately confirms it (no separate Confirm button). Defaults to false. */
    autoConfirmCell?: boolean;
  }

  let { locations, value, onSelect, onClose, initialLocationId = undefined, allowLocationOnly = true, autoConfirmCell = false }: Props = $props();

  // ── Screen state ─────────────────────────────────────────────────────────
  type Screen = 'tree' | 'cell' | 'camera';
  let screen = $state<Screen>('tree');

  // If an initial location is provided, skip the tree and jump to cell selection.
  $effect(() => {
    if (initialLocationId) {
      const loc = locations.find(l => l.id === initialLocationId);
      if (loc?.grid_config) {
        pendingLocation = loc;
        address = [];
        currentCell = null;
        screen = 'cell';
      }
    }
  });
  let searchQuery = $state('');
  let scanError = $state<string | null>(null);

  // Cell selection state
  let pendingLocation = $state<StorageLocation | null>(null);
  let address = $state<number[][]>([]);
  let currentCell = $state<number[] | null>(null);

  // Camera
  let videoEl = $state<HTMLVideoElement | null>(null);
  const scanner = createScanner(() => videoEl);

  $effect(() => {
    const deviceId = scanner.selectedCameraId;
    if (screen === 'camera' && videoEl) {
      scanner.startCamera(deviceId);
      return () => scanner.stopCamera();
    }
  });

  // ── Filtered tree ─────────────────────────────────────────────────────────
  const filteredLocations = $derived.by(() => {
    if (!searchQuery.trim()) return locations;
    const q = searchQuery.toLowerCase();
    const matchIds = new Set(locations.filter(l => l.label.toLowerCase().includes(q)).map(l => l.id));
    // Include ancestors of matched nodes
    const result = new Set(matchIds);
    for (const id of matchIds) {
      let cur = locations.find(l => l.id === id);
      while (cur?.parent_id) {
        result.add(cur.parent_id);
        cur = locations.find(l => l.id === cur!.parent_id);
      }
    }
    return locations.filter(l => result.has(l.id));
  });

  // ── Grid levels ───────────────────────────────────────────────────────────
  const gridConfig = $derived(pendingLocation?.grid_config as GridConfig | null);
  const totalLevels = $derived(gridConfig?.levels.length ?? 0);
  const currentLevelIndex = $derived(address.length);
  const isLastLevel = $derived(currentLevelIndex >= totalLevels - 1);

  // ── Tree interactions ─────────────────────────────────────────────────────
  function handleLocationSelect(loc: StorageLocation) {
    if (loc.grid_config) {
      pendingLocation = loc;
      address = [];
      currentCell = null;
      screen = 'cell';
    } else {
      onSelect({ id: loc.id, coordinate: null });
      onClose();
    }
  }

  // ── Cell interactions ─────────────────────────────────────────────────────
  function handleCellTap(coord: number[]) {
    currentCell = coord;
    if (autoConfirmCell) confirmCell();
  }

  function confirmCell() {
    if (!currentCell || !pendingLocation) return;
    if (isLastLevel) {
      const flat = [...address.flatMap(a => a), ...currentCell];
      onSelect({ id: pendingLocation.id, coordinate: flat });
      onClose();
    } else {
      address = [...address, currentCell];
      currentCell = null;
    }
  }

  function goBackInGrid() {
    if (address.length > 0) {
      address = address.slice(0, -1);
      currentCell = null;
    } else {
      screen = 'tree';
      pendingLocation = null;
    }
  }

  function useLocationOnly() {
    if (!pendingLocation) return;
    onSelect({ id: pendingLocation.id, coordinate: null });
    onClose();
  }

  // ── Camera scan ───────────────────────────────────────────────────────────
  async function handleCameraScan(raw: string): Promise<void> {
    scanError = null;
    const loc = parseStorageLocationQR(raw);
    if (!loc) {
      scanError = 'Not a location code';
      return;
    }

    const found = locations.find(l => l.id === loc.id);
    if (!found) {
      scanError = 'Location not found';
      return;
    }

    if (found.grid_config && !loc.coordinate) {
      // Grid location but no coordinate — pre-select and go to cell screen
      screen = 'cell';
      pendingLocation = found;
      address = [];
      currentCell = null;
      return;
    }

    // Full result or non-grid location
    const coordinate = loc.coordinate ?? null;
    onSelect({ id: found.id, coordinate });
    onClose();
  }
</script>

<div class="fixed inset-0 z-50 flex flex-col bg-zinc-950 text-white">

  {#if screen === 'camera'}
    <!-- ── Camera fullscreen ──────────────────────────────────────────────── -->
    <div class="flex flex-col flex-1 min-h-0">
      <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
        <button onclick={() => { screen = 'tree'; scanError = null; }} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <p class="font-medium">Scan Location QR</p>
      </div>

      <div class="relative flex-1 overflow-hidden">
        <!-- svelte-ignore a11y_media_has_caption -->
        <video bind:this={videoEl} autoplay playsinline muted class="h-full w-full object-cover"></video>

        <div class="absolute top-3 right-3 z-10">
          <CameraSelector {scanner} />
        </div>

        {#if scanner.cameraError}
          <div class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black/90 px-8 text-center">
            <p class="text-sm text-zinc-300">{scanner.cameraError}</p>
          </div>
        {:else}
          <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
            <div class="h-56 w-56 rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
          </div>
          {#if scanError}
            <div class="absolute top-4 left-4 right-4 rounded-xl bg-red-500/20 border border-red-500/30 px-4 py-3 text-center">
              <p class="text-sm text-red-300">{scanError}</p>
            </div>
          {/if}
          <div class="absolute bottom-0 left-0 right-0 flex justify-center pb-14">
            <button
              onpointerdown={(e) => { e.currentTarget.setPointerCapture(e.pointerId); scanner.startContinuousScan(handleCameraScan); }}
              onpointerup={() => scanner.stopContinuousScan()}
              onpointercancel={() => scanner.stopContinuousScan()}
              style="touch-action: none"
              class="px-14 py-5 rounded-full font-semibold text-lg transition-colors {scanner.isScanning ? 'bg-zinc-200 text-black' : 'bg-white text-black active:bg-zinc-200'}"
            >
              {scanner.isScanning ? 'Scanning…' : 'Hold to scan'}
            </button>
          </div>
        {/if}
      </div>
    </div>

  {:else if screen === 'cell' && pendingLocation && gridConfig}
    <!-- ── Cell selection ──────────────────────────────────────────────────── -->
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
      <button onclick={goBackInGrid} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <p class="font-medium truncate">{pendingLocation.label}</p>
      <p class="text-xs text-zinc-500 ml-auto shrink-0">
        Level {currentLevelIndex + 1}/{totalLevels}
      </p>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <p class="text-xs text-zinc-500">
        {#if gridConfig.levels[currentLevelIndex]?.name}
          Select {gridConfig.levels[currentLevelIndex].name}
        {:else}
          Select cell
        {/if}
      </p>

      <MobileGridVisualiser
        config={gridConfig}
        address={address}
        selected={currentCell}
        readonly={false}
        onCellTap={handleCellTap}
      />

      {#if !autoConfirmCell}
        {#if currentCell}
          <button
            onclick={confirmCell}
            class="w-full rounded-xl bg-white text-black font-semibold py-3 active:bg-zinc-200"
          >
            {isLastLevel ? 'Confirm' : 'Next Level →'}
          </button>
        {:else}
          <p class="text-center text-xs text-zinc-500">Tap a cell to select it</p>
        {/if}
      {:else}
        <p class="text-center text-xs text-zinc-500">Tap a cell to select it</p>
      {/if}

      {#if allowLocationOnly}
        <button
          onclick={useLocationOnly}
          class="w-full rounded-xl border border-zinc-700 py-2.5 text-sm text-zinc-400 active:bg-zinc-800"
        >
          Use location only (no cell)
        </button>
      {/if}
    </div>

  {:else}
    <!-- ── Tree screen ────────────────────────────────────────────────────── -->
    <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
      <p class="font-medium flex-1">Choose Location</p>
      <button
        onclick={() => { screen = 'camera'; scanError = null; }}
        aria-label="Scan location QR"
        class="p-2 text-zinc-400 active:text-white"
      >
        <!-- Camera icon -->
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M6.827 6.175A2.31 2.31 0 015.186 7.23c-.38.054-.757.112-1.134.175C2.999 7.58 2.25 8.507 2.25 9.574V18a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18V9.574c0-1.067-.75-1.994-1.802-2.169a47.865 47.865 0 00-1.134-.175 2.31 2.31 0 01-1.64-1.055l-.822-1.316a2.192 2.192 0 00-1.736-1.039 48.774 48.774 0 00-5.232 0 2.192 2.192 0 00-1.736 1.039l-.821 1.316z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M16.5 12.75a4.5 4.5 0 11-9 0 4.5 4.5 0 019 0zM18.75 10.5h.008v.008h-.008V10.5z" />
        </svg>
      </button>
      <button onclick={onClose} aria-label="Close" class="p-2 text-zinc-400 active:text-white">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
        </svg>
      </button>
    </div>

    <!-- Search -->
    <div class="px-4 py-2 border-b border-zinc-800 shrink-0">
      <input
        type="search"
        placeholder="Search locations…"
        bind:value={searchQuery}
        class="w-full rounded-lg bg-zinc-800 px-3 py-2 text-sm placeholder-zinc-500 outline-none focus:ring-1 focus:ring-zinc-600"
      />
    </div>

    <div class="flex-1 overflow-y-auto">
      <!-- None / clear option -->
      <button
        onclick={() => { onSelect(null); onClose(); }}
        class="flex w-full items-center gap-3 px-4 py-3 text-sm text-zinc-400 active:bg-zinc-800 border-b border-zinc-800"
      >
        <svg class="h-4 w-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
        </svg>
        None (clear location)
      </button>

      <!-- Current value indicator -->
      {#if value}
        <div class="px-4 py-2 border-b border-zinc-800 bg-zinc-900">
          <p class="text-xs text-zinc-500">Currently: {fullLocationLabel(value, locations)}</p>
        </div>
      {/if}

      <!-- Tree -->
      <div class="text-sm">
        <StorageTree
          locations={filteredLocations}
          selectedId={value?.id ?? null}
          onSelect={handleLocationSelect}
        />
      </div>

      {#if filteredLocations.length === 0}
        <p class="px-4 py-8 text-center text-sm text-zinc-500">No locations found</p>
      {/if}
    </div>
  {/if}
</div>

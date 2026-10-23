<script lang="ts">
  import { onMount } from 'svelte';
  import CameraViewport from './CameraViewport.svelte';
  import { scanFeedback, type ScanOutcome } from './feedback.svelte.js';
  import type { PartLocation } from './types.js';
  import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
  import { parseStorageLocationQR } from '$lib/utils/ipc2d.js';
  import {
    fullLocationLabel,
    locationPath,
    decomposeCoordinate,
    assembleCoordinate,
  } from './locationUtils.js';
  import {
    lookupBag,
    patchInventoryEntry,
    listScanStorageInventory,
    ScanExpiredError,
  } from '$lib/api/scanner.js';
  import MobileGridVisualiser from './MobileGridVisualiser.svelte';
  import MobileLocationPicker from './MobileLocationPicker.svelte';

  interface Props {
    locations: StorageLocation[];
    refreshLocations: () => Promise<void>;
    onBack: () => void;
    onExpired: () => void;
  }

  let { locations, refreshLocations, onBack, onExpired }: Props = $props();

  // Locations are fetched once at session pairing. Refresh on entry so a location
  // created/edited on the desktop since pairing is pickable here (the scan path
  // below additionally refreshes on a QR miss before giving up).
  onMount(() => { void refreshLocations(); });

  // ── State ─────────────────────────────────────────────────────────────────
  let selectedLocationId = $state<string | null>(null);
  let cursor = $state<number[] | null>(null);
  let cursorAddress = $state<number[][]>([]);
  let autoAdvance = $state(false);
  let direction = $state<'row' | 'col'>('row');
  let occupiedKeys = $state<Set<string>>(new Set());
  let loadingOccupied = $state(false);
  let showLocationPicker = $state(false);
  let pickerInitialLocationId = $state<string | null>(null);
  let toast = $state<{ msg: string; outcome: ScanOutcome } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  interface ConflictState {
    inventoryEntryId: string;
    currentLocation: PartLocation | null;
    newLocation: PartLocation;
    mpn: string | null;
  }
  let conflict = $state<ConflictState | null>(null);

  interface LogEntry { mpn: string | null; coord: number[] | null; }
  let log = $state<LogEntry[]>([]);

  // ── Derived ───────────────────────────────────────────────────────────────
  const location = $derived(selectedLocationId ? (locations.find(l => l.id === selectedLocationId) ?? null) : null);
  const isGridLocation = $derived(!!location?.grid_config);
  const gridConfig = $derived((location?.grid_config as GridConfig | null) ?? null);
  const totalLevels = $derived(gridConfig?.levels.length ?? 0);
  const currentLevel = $derived(gridConfig?.levels[totalLevels - 1] ?? null);

  // Last-level keys for the grid visualiser (strips parent-level prefix)
  const lastLevelOccupied = $derived.by(() => {
    if (!gridConfig || totalLevels === 0) return new Set<string>();
    const parentSize = cursorAddress.flatMap(a => a).length;
    const result = new Set<string>();
    for (const key of occupiedKeys) {
      const parts = key.split(',').map(Number);
      result.add(parts.slice(parentSize).join(','));
    }
    return result;
  });

  // Chip label shown in the target row
  const chipLabel = $derived.by(() => {
    if (!selectedLocationId) return 'Scan a location QR or tap to browse';
    const path = locationPath(selectedLocationId, locations);
    if (isGridLocation && cursor) {
      return `${path} / ${cellLabel(cursor)}`;
    }
    return path;
  });

  // ── Helpers ───────────────────────────────────────────────────────────────
  // Every scan outcome lands here, so this is also where it is announced.
  function showToast(msg: string, outcome: ScanOutcome) {
    scanFeedback(outcome);
    toast = { msg, outcome };
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = null; }, 2500);
  }

  async function loadOccupied(id: string) {
    loadingOccupied = true;
    occupiedKeys = new Set();
    try {
      const entries = await listScanStorageInventory(id);
      const keys = new Set<string>();
      for (const e of entries) {
        // The inventory query recurses into child locations, but only entries assigned
        // directly to THIS location share its grid's coordinate space. Filtering by id
        // prevents a descendant's coordinate from marking the wrong cell occupied.
        if (e.storage_location?.id === id && e.storage_location.coordinate) {
          keys.add(e.storage_location.coordinate.join(','));
        }
      }
      occupiedKeys = keys;
    } catch (e) {
      if (e instanceof ScanExpiredError) onExpired();
    } finally {
      loadingOccupied = false;
    }
  }

  /** Switch to a new location, resetting cursor/log state. */
  function switchLocation(newId: string) {
    const changed = newId !== selectedLocationId;
    selectedLocationId = newId;
    if (changed) {
      cursor = null;
      cursorAddress = [];
      log = [];
      const loc = locations.find(l => l.id === newId);
      if (loc?.grid_config) {
        loadOccupied(newId);
      } else {
        occupiedKeys = new Set();
        loadingOccupied = false;
      }
    }
  }

  function flatCoord(): number[] {
    if (!cursor) return [];
    return assembleCoordinate(cursorAddress, cursor);
  }

  function coordKey(coord: number[]): string {
    return coord.join(',');
  }

  function advanceCursor() {
    if (!cursor || !currentLevel) return;
    const rows = currentLevel.dims[0];
    const cols = currentLevel.dims.length > 1 ? currentLevel.dims[1] : 1;
    const is1D = currentLevel.dims.length === 1;

    if (is1D) {
      const next = cursor[0] + 1;
      cursor = next < rows ? [next] : [0];
    } else if (direction === 'row') {
      let r = cursor[0], c = cursor[1] ?? 0;
      c++;
      if (c >= cols) { c = 0; r++; }
      if (r >= rows) { r = 0; }
      cursor = [r, c];
    } else {
      let r = cursor[0], c = cursor[1] ?? 0;
      r++;
      if (r >= rows) { r = 0; c++; }
      if (c >= cols) { c = 0; }
      cursor = [r, c];
    }
  }

  function movePrev() {
    if (!cursor || !currentLevel) return;
    const rows = currentLevel.dims[0];
    const cols = currentLevel.dims.length > 1 ? currentLevel.dims[1] : 1;
    const is1D = currentLevel.dims.length === 1;

    if (is1D) {
      const prev = cursor[0] - 1;
      cursor = prev >= 0 ? [prev] : [rows - 1];
    } else if (direction === 'row') {
      let r = cursor[0], c = cursor[1] ?? 0;
      c--;
      if (c < 0) { c = cols - 1; r--; }
      if (r < 0) { r = rows - 1; }
      cursor = [r, c];
    } else {
      let r = cursor[0], c = cursor[1] ?? 0;
      r--;
      if (r < 0) { r = rows - 1; c--; }
      if (c < 0) { c = cols - 1; }
      cursor = [r, c];
    }
  }

  function cellLabel(coord: number[] | null): string {
    if (!coord || !gridConfig || !currentLevel) return coord?.join(',') ?? '—';
    const display = gridConfig.display ?? 'alphanumeric';
    const dims = currentLevel.dims.length;
    if (dims === 1) {
      return display === 'alphanumeric' ? String.fromCharCode(65 + coord[0]) : String(coord[0] + 1);
    }
    const row = display === 'alphanumeric' ? String.fromCharCode(65 + coord[0]) : String(coord[0] + 1);
    return `${row}${(coord[1] ?? 0) + 1}`;
  }

  // ── Scan handler ──────────────────────────────────────────────────────────
  async function handleScan(raw: string): Promise<void> {
    if (conflict) return;

    const loc = parseStorageLocationQR(raw);
    if (loc) {
      let found = locations.find(l => l.id === loc.id);
      if (!found) {
        // Possibly a location created after we paired — refresh once, then retry.
        await refreshLocations();
        found = locations.find(l => l.id === loc.id);
      }
      if (!found) { showToast('Location not found', 'error'); return; }

      const locationChanged = loc.id !== selectedLocationId;
      switchLocation(loc.id);

      if (loc.coordinate && loc.coordinate.length > 0 && found.grid_config) {
        // Full coordinate in QR — jump cursor directly
        const { address, cursor: cell } = decomposeCoordinate(loc.coordinate, found.grid_config);
        cursorAddress = address;
        cursor = cell;
        showToast(`Target: ${fullLocationLabel({ id: loc.id, coordinate: loc.coordinate }, locations)}`, 'success');
      } else if (found.grid_config) {
        // Grid location but no cell coord — open picker pre-focused if location changed
        showToast(`Target: ${locationPath(loc.id, locations)}`, 'success');
        if (locationChanged) {
          pickerInitialLocationId = loc.id;
          showLocationPicker = true;
        }
        // Same location re-scan: toast only, keep existing cursor
      } else {
        // Non-grid location — ready to scan bags immediately
        showToast(`Target: ${locationPath(loc.id, locations)}`, 'success');
      }
      return;
    }

    // ── Bag barcode ───────────────────────────────────────────────────────
    if (!selectedLocationId) { showToast('Scan a location first', 'warning'); return; }
    if (isGridLocation && !cursor) { showToast('Select a cell on the grid', 'warning'); return; }

    const flat = isGridLocation && cursor ? flatCoord() : null;
    const target: PartLocation = { id: selectedLocationId, coordinate: flat };

    try {
      const data = await lookupBag(raw);
      if (data.status !== 'found') { showToast('Part not found — import it first', 'error'); return; }
      const entryId = data.inventory_entry_id;
      if (!entryId) { showToast('No inventory entry for this part', 'error'); return; }

      const existing = data.storage_location;
      const sameLocation = existing?.id === target.id;
      const sameCoord = JSON.stringify(existing?.coordinate ?? null) === JSON.stringify(flat ?? null);

      if (existing && !(sameLocation && sameCoord)) {
        conflict = { inventoryEntryId: entryId, currentLocation: existing, newLocation: target, mpn: data.mpn };
        scanFeedback('warning');
        return;
      }

      await doAssign(entryId, target, data.mpn, flat);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      showToast('Error — try again', 'error');
    }
  }

  async function doAssign(entryId: string, target: PartLocation, mpn: string | null, flat: number[] | null) {
    await patchInventoryEntry(entryId, target);
    if (flat) {
      const key = coordKey(flat);
      occupiedKeys = new Set([...occupiedKeys, key]);
    }
    log = [{ mpn, coord: flat }, ...log].slice(0, 5);
    const cell = isGridLocation && cursor ? cellLabel(cursor) : null;
    showToast(cell ? `✓ ${mpn ?? '—'} → ${cell}` : `✓ ${mpn ?? '—'} stored`, 'success');
    if (autoAdvance && isGridLocation) advanceCursor();
  }

  async function confirmConflict() {
    if (!conflict) return;
    try {
      await doAssign(conflict.inventoryEntryId, conflict.newLocation, conflict.mpn, conflict.newLocation.coordinate);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      showToast('Could not move — try again', 'error');
    } finally {
      conflict = null;
    }
  }

  // ── Picker selection ──────────────────────────────────────────────────────
  function handlePickerSelect(loc: PartLocation | null) {
    if (!loc) {
      selectedLocationId = null;
      cursor = null;
      cursorAddress = [];
      log = [];
      occupiedKeys = new Set();
    } else {
      switchLocation(loc.id);
      if (loc.coordinate && loc.coordinate.length > 0) {
        const locData = locations.find(l => l.id === loc.id);
        if (locData?.grid_config) {
          const { address, cursor: cell } = decomposeCoordinate(loc.coordinate, locData.grid_config);
          cursorAddress = address;
          cursor = cell;
        }
      }
    }
    pickerInitialLocationId = null;
    showLocationPicker = false;
  }
</script>

<div class="flex flex-col flex-1 min-h-0">
  <!-- Header -->
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <p class="font-medium">Storage Assignment</p>
  </div>

  <!-- Camera -->
  <div class="relative overflow-hidden shrink-0" style="height: 45vh">
    <CameraViewport onScan={handleScan} reticleSize="sm">
      {#snippet topRightControls()}
        {#if isGridLocation && cursor !== null}
          <!-- Auto-advance toggle -->
          <button
            onclick={() => autoAdvance = !autoAdvance}
            aria-label={autoAdvance ? 'Auto-advance on' : 'Auto-advance off'}
            class="flex h-9 w-9 items-center justify-center rounded-full border active:opacity-70
              {autoAdvance
                ? 'bg-white/20 border-white/50 text-white'
                : 'bg-black/50 border-zinc-600 text-zinc-400'}"
          >
            <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5"
                d="M5.25 4.5l7.5 7.5-7.5 7.5m6-15l7.5 7.5-7.5 7.5" />
            </svg>
          </button>
          <!-- Direction toggle -->
          <button
            onclick={() => direction = direction === 'row' ? 'col' : 'row'}
            aria-label={direction === 'row' ? 'Row traversal' : 'Column traversal'}
            class="flex h-9 w-9 items-center justify-center rounded-full border bg-black/50 border-zinc-600 text-zinc-300 active:opacity-70"
          >
            {#if direction === 'row'}
              <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5"
                  d="M13.5 4.5L21 12m0 0l-7.5 7.5M21 12H3" />
              </svg>
            {:else}
              <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5"
                  d="M19.5 13.5L12 21m0 0l-7.5-7.5M12 21V3" />
              </svg>
            {/if}
          </button>
        {/if}
      {/snippet}

      {#snippet overlay()}
        {#if toast}
          {#key toast}
            <div class="scan-card pointer-events-none absolute top-3 left-3 right-3 rounded-xl border px-4 py-2 text-center text-sm font-medium
              {toast.outcome === 'success' ? 'bg-emerald-950/95 border-emerald-600 text-emerald-100'
                : toast.outcome === 'warning' ? 'bg-amber-950/95 border-amber-600 text-amber-100'
                : toast.outcome === 'error' ? 'bg-red-950/95 border-red-700 text-red-100'
                : 'bg-zinc-900/90 border-zinc-700 text-white'}">
              {toast.msg}
            </div>
          {/key}
        {/if}
      {/snippet}

      {#snippet bottomBar(scanner)}
        <!-- prev / scan / next -->
        <div class="absolute bottom-0 left-0 right-0 flex items-center justify-center gap-4 pb-4">
          {#if isGridLocation && cursor !== null}
            <button
              onclick={movePrev}
              aria-label="Previous cell"
              class="flex h-11 w-11 items-center justify-center rounded-full bg-black/60 border border-white/20 text-white active:bg-black/80"
            >
              <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M15 19l-7-7 7-7" />
              </svg>
            </button>
          {/if}

          <button
            onpointerdown={(e) => { e.currentTarget.setPointerCapture(e.pointerId); scanner.startContinuousScan(handleScan); }}
            onpointerup={() => scanner.stopContinuousScan()}
            onpointercancel={() => scanner.stopContinuousScan()}
            style="touch-action: none"
            class="px-10 py-3.5 rounded-full font-semibold transition-colors {scanner.isScanning ? 'bg-zinc-200 text-black' : 'bg-white text-black active:bg-zinc-200'}"
          >
            {scanner.isScanning ? 'Scanning…' : 'Hold to scan'}
          </button>

          {#if isGridLocation && cursor !== null}
            <button
              onclick={advanceCursor}
              aria-label="Next cell"
              class="flex h-11 w-11 items-center justify-center rounded-full bg-black/60 border border-white/20 text-white active:bg-black/80"
            >
              <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" />
              </svg>
            </button>
          {/if}
        </div>
      {/snippet}
    </CameraViewport>
  </div>

  <!-- Bottom panel -->
  <div class="flex-1 overflow-y-auto p-4 space-y-3 border-t border-zinc-800">

    <!-- Target chip -->
    <div class="rounded-xl bg-zinc-900 p-3">
      <p class="text-xs text-zinc-500 mb-1.5">Assign to</p>
      <button
        onclick={() => { pickerInitialLocationId = null; showLocationPicker = true; }}
        class="flex w-full items-center gap-2 text-left active:opacity-70"
      >
        <svg class="h-4 w-4 shrink-0 text-zinc-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M15 10.5a3 3 0 11-6 0 3 3 0 016 0z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M19.5 10.5c0 7.142-7.5 11.25-7.5 11.25S4.5 17.642 4.5 10.5a7.5 7.5 0 1115 0z" />
        </svg>
        <span class="text-sm {selectedLocationId ? 'font-medium' : 'text-zinc-400'} flex-1 truncate">
          {chipLabel}
        </span>
        <svg class="h-4 w-4 shrink-0 text-zinc-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
      </button>
    </div>

    <!-- Grid section — only when a grid location is active with a cell selected -->
    {#if isGridLocation && cursor !== null}
      <div class="space-y-2">
        <!-- Grid visualiser with loading overlay -->
        <div class="relative">
          <MobileGridVisualiser
            config={gridConfig!}
            address={cursorAddress}
            selected={cursor}
            occupied={lastLevelOccupied}
            readonly={loadingOccupied}
            onCellTap={(coord) => { cursor = coord; }}
          />
          {#if loadingOccupied}
            <div class="absolute inset-0 flex items-center justify-center rounded-xl bg-black/50">
              <div class="h-5 w-5 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
            </div>
          {/if}
        </div>

      </div>
    {/if}

    <!-- Session log -->
    {#if log.length > 0}
      <div class="space-y-1 pt-1">
        {#each log as entry}
          <div class="flex items-center gap-2 text-xs text-zinc-400">
            <svg class="h-3 w-3 text-green-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
            </svg>
            <span class="font-mono">{entry.mpn ?? '—'}</span>
            {#if entry.coord}
              <span class="ml-auto text-zinc-600">{cellLabel(entry.coord)}</span>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<!-- Conflict modal — fixed overlay so it's always visible regardless of scroll position -->
{#if conflict}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-6">
    <div class="w-full max-w-sm rounded-2xl bg-zinc-900 p-6 space-y-4">
      <p class="font-semibold">Move part?</p>
      {#if conflict.mpn}
        <p class="font-mono text-sm text-zinc-300">{conflict.mpn}</p>
      {/if}
      <div class="text-sm space-y-1 text-zinc-400">
        <p>Was: <span class="text-white">{conflict.currentLocation ? fullLocationLabel(conflict.currentLocation, locations) : '—'}</span></p>
        <p>Now: <span class="text-white">{fullLocationLabel(conflict.newLocation, locations)}</span></p>
      </div>
      <div class="flex gap-3">
        <button
          onclick={() => conflict = null}
          class="flex-1 rounded-xl border border-zinc-700 py-3 text-sm font-semibold text-zinc-300 active:bg-zinc-800"
        >
          Skip
        </button>
        <button
          onclick={confirmConflict}
          class="flex-1 rounded-xl bg-white py-3 text-sm font-semibold text-black active:bg-zinc-200"
        >
          Confirm →
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Location picker modal -->
{#if showLocationPicker}
  <MobileLocationPicker
    {locations}
    value={selectedLocationId ? { id: selectedLocationId, coordinate: null } : null}
    initialLocationId={pickerInitialLocationId ?? undefined}
    allowLocationOnly={false}
    autoConfirmCell={true}
    onSelect={handlePickerSelect}
    onClose={() => { showLocationPicker = false; pickerInitialLocationId = null; }}
  />
{/if}

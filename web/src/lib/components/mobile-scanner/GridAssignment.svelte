<script lang="ts">
  import { untrack } from 'svelte';
  import { createScanner } from './scanner.svelte.js';
  import type { PartLocation } from './types.js';
  import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
  import { parseStorageLocationQR } from '$lib/utils/ipc2d.js';
  import { fullLocationLabel } from './locationUtils.js';
  import { lookupBag, patchInventoryEntry, listScanStorageInventory, ScanExpiredError } from '$lib/api/scanner.js';
  import MobileGridVisualiser from './MobileGridVisualiser.svelte';
  import MobileLocationPicker from './MobileLocationPicker.svelte';

  interface Props {
    token: string;
    locations: StorageLocation[];
    initialLocationId?: string;
    onBack: () => void;
    onExpired: () => void;
  }

  let { token, locations, initialLocationId = undefined, onBack, onExpired }: Props = $props();

  // ── Camera ────────────────────────────────────────────────────────────────
  let videoEl = $state<HTMLVideoElement | null>(null);
  const scanner = createScanner(() => videoEl);

  $effect(() => {
    if (videoEl) {
      scanner.startCamera();
      return () => scanner.stopCamera();
    }
  });

  // ── State ─────────────────────────────────────────────────────────────────
  // initialLocationId is intentionally a one-time seed — untrack so Svelte
  // doesn't warn about capturing a reactive prop's initial value.
  let selectedLocationId = $state<string | null>(untrack(() => initialLocationId ?? null));
  let showLocationPicker = $state(false);
  let toast = $state<string | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Grid cursor
  let cursor = $state<number[] | null>(null);           // current cell coordinate at last level
  let cursorAddress = $state<number[][]>([]);           // confirmed parent level selections
  let autoAdvance = $state(false);
  let direction = $state<'row' | 'col'>('row');         // row-major or col-major

  // Occupied cells (pre-loaded + session-filled)
  let occupiedKeys = $state<Set<string>>(new Set());   // coordinate.join(',')
  let sessionKeys = $state<Set<string>>(new Set());    // assigned this session

  // Conflict state
  interface ConflictState {
    inventoryEntryId: string;
    currentLocation: PartLocation | null;
    mpn: string | null;
  }
  let conflict = $state<ConflictState | null>(null);

  // Running log
  interface LogEntry { mpn: string | null; coord: number[]; }
  let log = $state<LogEntry[]>([]);

  // ── Location / grid derived ───────────────────────────────────────────────
  const location = $derived(selectedLocationId ? locations.find(l => l.id === selectedLocationId) : null);
  const gridConfig = $derived(location?.grid_config as GridConfig | null ?? null);
  const totalLevels = $derived(gridConfig?.levels.length ?? 0);

  function locationPath(id: string): string {
    const path: string[] = [];
    let cur = locations.find(l => l.id === id);
    while (cur) {
      path.unshift(cur.label);
      cur = cur.parent_id ? locations.find(l => l.id === cur!.parent_id) : undefined;
    }
    return path.join(' → ');
  }

  // ── Load occupied cells when location changes ────────────────────────────
  $effect(() => {
    if (!selectedLocationId) return;
    const id = selectedLocationId;
    listScanStorageInventory(token, id)
      .then(entries => {
        const keys = new Set<string>();
        for (const e of entries) {
          if (e.storage_location?.coordinate) {
            keys.add(e.storage_location.coordinate.join(','));
          }
        }
        occupiedKeys = keys;
      })
      .catch(e => {
        if (e instanceof ScanExpiredError) onExpired();
      });
  });

  // Reset cursor when location changes
  $effect(() => {
    if (selectedLocationId) {
      cursor = null;
      cursorAddress = [];
      conflict = null;
    }
  });

  function showToast(msg: string) {
    toast = msg;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = null; }, 2500);
  }

  // ── Grid cursor helpers ───────────────────────────────────────────────────
  // Current level = last level (we navigate level by level via cursorAddress)
  // For simplicity: we support multi-level grids but the cursor is always at the deepest level.
  // The address (parent levels) is set when a location QR with full coords is scanned.

  const currentLevel = $derived(gridConfig?.levels[totalLevels - 1] ?? null);

  function flatCoord(): number[] {
    if (!cursor) return [];
    return [...cursorAddress.flatMap(a => a), ...cursor];
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

  // ── Scan handler ──────────────────────────────────────────────────────────
  async function handleScan(raw: string): Promise<void> {
    if (conflict) return;

    // 1. Location QR
    const loc = parseStorageLocationQR(raw);
    if (loc) {
      const found = locations.find(l => l.id === loc.id);
      if (!found) { showToast('Location not found'); return; }

      if (loc.id === selectedLocationId) {
        if (loc.coordinate && loc.coordinate.length > 0) {
          // Jump cursor to that cell (last-level portion)
          const lastDims = gridConfig?.levels[totalLevels - 1]?.dims.length ?? 1;
          const parentSize = (loc.coordinate.length - lastDims);
          cursor = loc.coordinate.slice(parentSize);
          if (parentSize > 0) {
            // Reconstruct parent address
            let off = 0;
            const addr: number[][] = [];
            for (let i = 0; i < totalLevels - 1; i++) {
              const d = gridConfig!.levels[i].dims.length;
              addr.push(loc.coordinate.slice(off, off + d));
              off += d;
            }
            cursorAddress = addr;
          }
        } else {
          showToast('No cell info in QR — navigate manually');
        }
      } else {
        showToast(`Switch target to ${found.label}?`);
        // Simple auto-switch (no confirm needed per spec's simplified variant)
        selectedLocationId = loc.id;
        if (loc.coordinate && loc.coordinate.length > 0) {
          const lastDims = gridConfig?.levels[totalLevels - 1]?.dims.length ?? 1;
          cursor = loc.coordinate.slice(loc.coordinate.length - lastDims);
        }
      }
      return;
    }

    // 2. Bag barcode
    if (!selectedLocationId || !cursor) {
      showToast(selectedLocationId ? 'Tap a cell to start' : 'Select a location first');
      return;
    }

    const flat = flatCoord();
    const target: PartLocation = { id: selectedLocationId, coordinate: flat };

    try {
      const data = await lookupBag(token, raw);
      if (data.status !== 'found') {
        showToast('Part not found — import it first');
        return;
      }

      const entryId = data.inventory_entry_id;
      if (!entryId) { showToast('No inventory entry for this part'); return; }

      const existing = data.storage_location;
      const sameLocation = existing?.id === target.id;
      const sameCoord = JSON.stringify(existing?.coordinate ?? null) === JSON.stringify(flat);

      if (existing && !(sameLocation && sameCoord)) {
        conflict = { inventoryEntryId: entryId, currentLocation: existing, mpn: data.mpn };
        return;
      }

      await assignToCell(entryId, target, data.mpn, flat);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      showToast('Error — try again');
    }
  }

  async function assignToCell(entryId: string, target: PartLocation, mpn: string | null, flat: number[]) {
    await patchInventoryEntry(token, entryId, target);
    const key = coordKey(flat);
    occupiedKeys = new Set([...occupiedKeys, key]);
    sessionKeys = new Set([...sessionKeys, key]);
    log = [{ mpn, coord: flat }, ...log].slice(0, 5);
    if (autoAdvance) advanceCursor();
  }

  async function confirmConflict() {
    if (!conflict || !cursor || !selectedLocationId) return;
    const flat = flatCoord();
    const target: PartLocation = { id: selectedLocationId, coordinate: flat };
    try {
      await assignToCell(conflict.inventoryEntryId, target, conflict.mpn, flat);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    } finally {
      conflict = null;
    }
  }

  function cellLabel(coord: number[]): string {
    if (!gridConfig || !currentLevel) return coord.join(',');
    const display = gridConfig.display ?? 'alphanumeric';
    const dims = currentLevel.dims.length;
    if (dims === 1) {
      return display === 'alphanumeric' ? String.fromCharCode(65 + coord[0]) : String(coord[0] + 1);
    }
    const row = display === 'alphanumeric' ? String.fromCharCode(65 + coord[0]) : String(coord[0] + 1);
    return `${row}${(coord[1] ?? 0) + 1}`;
  }

  // Build occupied set for MobileGridVisualiser (last-level only)
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

  const lastLevelSession = $derived.by(() => {
    if (!gridConfig || totalLevels === 0) return new Set<string>();
    const parentSize = cursorAddress.flatMap(a => a).length;
    const result = new Set<string>();
    for (const key of sessionKeys) {
      const parts = key.split(',').map(Number);
      result.add(parts.slice(parentSize).join(','));
    }
    return result;
  });
</script>

<div class="flex flex-col flex-1 min-h-0">
  <!-- Header -->
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <p class="font-medium">Grid Assignment</p>
  </div>

  <!-- Camera (top half) -->
  <div class="relative overflow-hidden shrink-0" style="height: 45vh">
    <!-- svelte-ignore a11y_media_has_caption -->
    <video bind:this={videoEl} autoplay playsinline muted class="h-full w-full object-cover"></video>

    {#if scanner.cameraError}
      <div class="absolute inset-0 flex items-center justify-center bg-black/80 px-8 text-center">
        <p class="text-sm text-zinc-300">{scanner.cameraError}</p>
      </div>
    {:else}
      <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
        <div class="h-36 w-36 rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
      </div>
      {#if toast}
        <div class="pointer-events-none absolute top-3 left-3 right-3 rounded-xl bg-zinc-900/90 px-4 py-2 text-center text-sm text-white">
          {toast}
        </div>
      {/if}
      <div class="absolute bottom-0 left-0 right-0 flex justify-center pb-4">
        <button
          onclick={() => scanner.triggerScan(handleScan)}
          disabled={scanner.scanState === 'decoding'}
          class="px-10 py-3.5 rounded-full bg-white text-black font-semibold active:bg-zinc-200 disabled:opacity-60"
        >
          {scanner.scanState === 'decoding' ? 'Scanning…' : 'Scan'}
        </button>
      </div>
    {/if}
  </div>

  <!-- Grid panel (bottom) -->
  <div class="flex-1 overflow-y-auto p-4 space-y-3 border-t border-zinc-800">

    {#if !selectedLocationId}
      <!-- No location selected -->
      <div class="flex flex-col items-center gap-4 py-4 text-center">
        <p class="text-sm text-zinc-400">Scan a grid location QR</p>
        <p class="text-xs text-zinc-600">or</p>
        <button
          onclick={() => showLocationPicker = true}
          class="rounded-xl border border-zinc-700 px-6 py-2.5 text-sm text-zinc-300 active:bg-zinc-800"
        >
          Browse…
        </button>
      </div>

    {:else if !gridConfig}
      <!-- Location has no grid -->
      <p class="text-sm text-zinc-500 text-center py-4">
        {location?.label} has no grid. Use Batch Putaway instead.
      </p>

    {:else}
      <!-- Location + grid -->
      <div class="flex items-baseline justify-between">
        <button onclick={() => showLocationPicker = true} class="text-sm font-medium truncate text-left flex-1 active:opacity-70">
          {locationPath(selectedLocationId)}
        </button>
        {#if cursor}
          <span class="ml-2 shrink-0 font-mono text-sm text-zinc-300">{cellLabel(cursor)}</span>
        {/if}
      </div>

      <!-- Grid visualiser -->
      {#if gridConfig}
        <MobileGridVisualiser
          config={gridConfig}
          address={cursorAddress}
          selected={cursor}
          occupied={lastLevelOccupied}
          readonly={false}
          onCellTap={(coord) => { cursor = coord; }}
        />
      {/if}

      {#if !cursor}
        <p class="text-center text-xs text-zinc-500">Tap a cell to begin</p>
      {/if}

      <!-- Conflict card -->
      {#if conflict}
        <div class="rounded-xl bg-amber-500/10 border border-amber-500/30 p-3 space-y-2">
          <p class="text-xs font-medium text-amber-400">Location conflict</p>
          <div class="text-sm text-zinc-400">
            Was: <span class="text-white">{conflict.currentLocation ? fullLocationLabel(conflict.currentLocation, locations) : '—'}</span>
          </div>
          <div class="flex gap-2">
            <button onclick={confirmConflict} class="flex-1 rounded-lg bg-white py-2 text-sm font-semibold text-black">Confirm</button>
            <button onclick={() => conflict = null} class="flex-1 rounded-lg border border-zinc-700 py-2 text-sm font-semibold text-zinc-300">Skip</button>
          </div>
        </div>
      {/if}

      <!-- Controls -->
      {#if cursor}
        <div class="grid grid-cols-2 gap-2">
          <button onclick={movePrev} class="rounded-xl border border-zinc-700 py-2.5 text-sm text-zinc-300 active:bg-zinc-800">
            ← Prev
          </button>
          <button onclick={advanceCursor} class="rounded-xl border border-zinc-700 py-2.5 text-sm text-zinc-300 active:bg-zinc-800">
            Next →
          </button>
        </div>
        <div class="grid grid-cols-2 gap-2">
          <button
            onclick={() => autoAdvance = !autoAdvance}
            class="rounded-xl py-2.5 text-sm font-medium active:opacity-70 {autoAdvance ? 'bg-white text-black' : 'border border-zinc-700 text-zinc-300'}"
          >
            {autoAdvance ? '⏭ Auto On' : '⏭ Auto Off'}
          </button>
          <button
            onclick={() => direction = direction === 'row' ? 'col' : 'row'}
            class="rounded-xl border border-zinc-700 py-2.5 text-sm text-zinc-300 active:bg-zinc-800"
          >
            {direction === 'row' ? '→ Row' : '↓ Col'}
          </button>
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
              <span class="ml-auto text-zinc-600">{cellLabel(entry.coord)}</span>
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
</div>

{#if showLocationPicker}
  <MobileLocationPicker
    {locations}
    value={selectedLocationId ? { id: selectedLocationId, coordinate: null } : null}
    onSelect={(loc) => {
      if (loc) {
        selectedLocationId = loc.id;
        cursor = null;
        cursorAddress = [];
      }
    }}
    onClose={() => showLocationPicker = false}
  />
{/if}

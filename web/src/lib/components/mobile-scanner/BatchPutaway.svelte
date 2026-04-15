<script lang="ts">
  import { createScanner } from './scanner.svelte.js';
  import type { PartLocation } from './types.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { parseStorageLocationQR } from '$lib/utils/ipc2d.js';
  import { lookupBag, patchInventoryEntry, ScanExpiredError } from '$lib/api/scanner.js';
  import { fullLocationLabel } from './locationUtils.js';
  import MobileLocationPicker from './MobileLocationPicker.svelte';

  interface Props {
    token: string;
    locations: StorageLocation[];
    onBack: () => void;
    onExpired: () => void;
  }

  let { token, locations, onBack, onExpired }: Props = $props();

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
  let target = $state<PartLocation | null>(null);
  let showLocationPicker = $state(false);
  let toast = $state<string | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;

  // Running log of assigned parts (last 3 shown)
  interface LogEntry { mpn: string | null; }
  let log = $state<LogEntry[]>([]);
  let assignedCount = $state(0);

  // Conflict state
  interface ConflictState {
    inventoryEntryId: string;
    currentLocation: PartLocation | null;
    newLocation: PartLocation;
    mpn: string | null;
  }
  let conflict = $state<ConflictState | null>(null);

  // ── Location helpers ──────────────────────────────────────────────────────
  function targetNeedsCell(): boolean {
    if (!target) return false;
    const loc = locations.find(l => l.id === target!.id);
    return !!(loc?.grid_config && !target.coordinate);
  }

  function targetLabel(): string {
    if (!target) return '';
    return fullLocationLabel(target, locations);
  }

  function showToast(msg: string) {
    toast = msg;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = null; }, 2500);
  }

  // ── Scan handler ──────────────────────────────────────────────────────────
  async function handleScan(raw: string): Promise<void> {
    // If conflict card is showing, ignore new scans
    if (conflict) return;

    // 1. Check for location QR
    const loc = parseStorageLocationQR(raw);
    if (loc) {
      const found = locations.find(l => l.id === loc.id);
      if (!found) { showToast('Location not found'); return; }

      if (found.grid_config && !loc.coordinate) {
        target = { id: loc.id, coordinate: null };
        showToast(`Tap chip to select a cell in ${found.label}`);
      } else {
        target = { id: loc.id, coordinate: loc.coordinate ?? null };
        showToast(`Target: ${fullLocationLabel({ id: loc.id, coordinate: loc.coordinate ?? null }, locations)}`);
      }
      return;
    }

    // 2. Bag barcode
    if (!target || targetNeedsCell()) {
      showToast(target ? 'Select a cell first' : 'Set a target location first');
      return;
    }

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
      const sameCoord = JSON.stringify(existing?.coordinate ?? null) === JSON.stringify(target.coordinate ?? null);

      if (existing && !(sameLocation && sameCoord)) {
        // Conflict
        conflict = {
          inventoryEntryId: entryId,
          currentLocation: existing,
          newLocation: target,
          mpn: data.mpn,
        };
        return;
      }

      // No conflict — assign silently
      await patchInventoryEntry(token, entryId, target);
      log = [{ mpn: data.mpn }, ...log].slice(0, 3);
      assignedCount++;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      showToast('Error — try again');
    }
  }

  async function confirmConflict() {
    if (!conflict || !target) return;
    try {
      await patchInventoryEntry(token, conflict.inventoryEntryId, target);
      log = [{ mpn: conflict.mpn }, ...log].slice(0, 3);
      assignedCount++;
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    } finally {
      conflict = null;
    }
  }

  function skipConflict() {
    conflict = null;
  }

  function locationChipLabel(): string {
    if (!target) return 'Scan a location QR or tap to set target';
    if (targetNeedsCell()) {
      const loc = locations.find(l => l.id === target!.id);
      return `…/${loc?.label ?? ''} — tap to select cell`;
    }
    return targetLabel();
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
    <p class="font-medium">Batch Putaway</p>
  </div>

  <!-- Camera (top half) -->
  <div class="relative flex-1 overflow-hidden" style="max-height: 50vh">
    <!-- svelte-ignore a11y_media_has_caption -->
    <video bind:this={videoEl} autoplay playsinline muted class="h-full w-full object-cover"></video>

    {#if scanner.cameraError}
      <div class="absolute inset-0 flex items-center justify-center bg-black/80 px-8 text-center">
        <p class="text-sm text-zinc-300">{scanner.cameraError}</p>
      </div>
    {:else}
      <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
        <div class="h-40 w-40 rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
      </div>
      <!-- Toast -->
      {#if toast}
        <div class="pointer-events-none absolute top-3 left-3 right-3 rounded-xl bg-zinc-900/90 px-4 py-2 text-center text-sm text-white">
          {toast}
        </div>
      {/if}
      <div class="absolute bottom-0 left-0 right-0 flex justify-center pb-6">
        <button
          onclick={() => scanner.triggerScan(handleScan)}
          disabled={scanner.scanState === 'decoding'}
          class="px-10 py-4 rounded-full bg-white text-black font-semibold active:bg-zinc-200 disabled:opacity-60"
        >
          {scanner.scanState === 'decoding' ? 'Scanning…' : 'Scan'}
        </button>
      </div>
    {/if}
  </div>

  <!-- Context panel (bottom half) -->
  <div class="flex-1 overflow-y-auto p-4 space-y-3 border-t border-zinc-800">

    <!-- Target chip -->
    <div class="rounded-xl bg-zinc-900 p-3">
      <p class="text-xs text-zinc-500 mb-1.5">Assign to</p>
      <button
        onclick={() => showLocationPicker = true}
        class="flex w-full items-center gap-2 text-left active:opacity-70"
      >
        <svg class="h-4 w-4 shrink-0 text-zinc-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M15 10.5a3 3 0 11-6 0 3 3 0 016 0z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M19.5 10.5c0 7.142-7.5 11.25-7.5 11.25S4.5 17.642 4.5 10.5a7.5 7.5 0 1115 0z" />
        </svg>
        <span class="text-sm {target && !targetNeedsCell() ? 'font-medium' : 'text-zinc-400'} flex-1 truncate">
          {locationChipLabel()}
        </span>
        <svg class="h-4 w-4 shrink-0 text-zinc-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
      </button>
    </div>

    <!-- Conflict card -->
    {#if conflict}
      <div class="rounded-xl bg-amber-500/10 border border-amber-500/30 p-4 space-y-2">
        <p class="text-xs font-medium text-amber-400">Location conflict</p>
        <div class="text-sm space-y-0.5">
          <p class="text-zinc-400">Was: <span class="text-white">{conflict.currentLocation ? fullLocationLabel(conflict.currentLocation, locations) : '—'}</span></p>
          <p class="text-zinc-400">Now: <span class="text-white">{fullLocationLabel(conflict.newLocation, locations)}</span></p>
        </div>
        <div class="flex gap-3 pt-1">
          <button onclick={confirmConflict} class="flex-1 rounded-lg bg-white py-2 text-sm font-semibold text-black active:bg-zinc-200">
            Confirm
          </button>
          <button onclick={skipConflict} class="flex-1 rounded-lg border border-zinc-700 py-2 text-sm font-semibold text-zinc-300 active:bg-zinc-800">
            Skip
          </button>
        </div>
      </div>
    {/if}

    <!-- Running log -->
    {#if log.length > 0}
      <div class="space-y-1">
        {#each log as entry}
          <div class="flex items-center gap-2 text-sm">
            <svg class="h-3.5 w-3.5 text-green-400 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
            </svg>
            <span class="font-mono text-zinc-300">{entry.mpn ?? '—'}</span>
          </div>
        {/each}
      </div>
    {/if}

    {#if assignedCount > 0}
      <p class="text-xs text-zinc-500">{assignedCount} assigned</p>
    {/if}
  </div>
</div>

{#if showLocationPicker}
  <MobileLocationPicker
    {locations}
    value={target}
    onSelect={(loc) => { target = loc; }}
    onClose={() => showLocationPicker = false}
  />
{/if}

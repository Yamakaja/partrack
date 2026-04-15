<script lang="ts">
  import { onMount } from 'svelte';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import type { ScanPart } from './types.js';
  import { listScanStorageInventory, ScanExpiredError, type StorageInventoryEntry } from '$lib/api/scanner.js';
  import MobileGridVisualiser from './MobileGridVisualiser.svelte';

  interface Props {
    token: string;
    locationId: string;
    coordinate?: number[] | null;
    locations: StorageLocation[];
    onBack: () => void;
    onPartSelect: (part: ScanPart) => void;
    onExpired: () => void;
  }

  let { token, locationId, coordinate, locations, onBack, onPartSelect, onExpired }: Props = $props();

  let entries = $state<StorageInventoryEntry[]>([]);
  let loading = $state(true);
  let searchQuery = $state('');

  // ── Location path ─────────────────────────────────────────────────────────
  function locationPath(id: string): string {
    const path: string[] = [];
    let cur = locations.find(l => l.id === id);
    while (cur) {
      path.unshift(cur.label);
      cur = cur.parent_id ? locations.find(l => l.id === cur!.parent_id) : undefined;
    }
    return path.join(' → ');
  }

  const location = $derived(locations.find(l => l.id === locationId));
  const breadcrumb = $derived(locationPath(locationId));

  // ── Grid config for coordinate display ────────────────────────────────────
  const gridConfig = $derived(location?.grid_config ?? null);

  // For MobileGridVisualiser: derive address and selected from the coordinate.
  // Coordinate is flat: [l0r, l0c?, l1r, l1c?, ...]. We need to split per level.
  const gridAddress = $derived.by((): number[][] => {
    if (!gridConfig || !coordinate) return [];
    const levels = gridConfig.levels;
    const addr: number[][] = [];
    let offset = 0;
    // All levels except the last are "parent" levels (address)
    for (let i = 0; i < levels.length - 1; i++) {
      const dims = levels[i].dims.length;
      addr.push(coordinate.slice(offset, offset + dims));
      offset += dims;
    }
    return addr;
  });

  const gridSelected = $derived.by((): number[] | null => {
    if (!gridConfig || !coordinate) return null;
    const levels = gridConfig.levels;
    if (levels.length === 0) return null;
    // The last level selection
    let offset = 0;
    for (let i = 0; i < levels.length - 1; i++) {
      offset += levels[i].dims.length;
    }
    const lastLevel = levels[levels.length - 1];
    return coordinate.slice(offset, offset + lastLevel.dims.length);
  });

  // ── Filtered entries ──────────────────────────────────────────────────────
  const filteredEntries = $derived.by(() => {
    let list = entries;
    // If scoped to a coordinate, filter to exact matches
    if (coordinate && coordinate.length > 0) {
      const key = coordinate.join(',');
      list = list.filter(e =>
        e.storage_location?.coordinate?.join(',') === key,
      );
    }
    if (!searchQuery.trim()) return list;
    const q = searchQuery.toLowerCase();
    return list.filter(
      e =>
        (e.mpn ?? '').toLowerCase().includes(q) ||
        (e.description ?? '').toLowerCase().includes(q),
    );
  });

  // ── Coord label ───────────────────────────────────────────────────────────
  function coordLabel(coord: number[] | null): string | null {
    if (!coord || coord.length === 0 || !gridConfig) return null;
    const levels = gridConfig.levels;
    const display = gridConfig.display ?? 'alphanumeric';
    const parts: string[] = [];
    let offset = 0;
    for (const level of levels) {
      const dims = level.dims.length;
      const slice = coord.slice(offset, offset + dims);
      if (slice.length === 0) break;
      if (dims === 1) {
        parts.push(display === 'alphanumeric' ? String.fromCharCode(65 + slice[0]) : String(slice[0] + 1));
      } else {
        const row = display === 'alphanumeric' ? String.fromCharCode(65 + slice[0]) : String(slice[0] + 1);
        const col = String(slice[1] + 1);
        parts.push(`${row}${col}`);
      }
      offset += dims;
    }
    return parts.join('/');
  }

  onMount(async () => {
    try {
      entries = await listScanStorageInventory(token, locationId);
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
    } finally {
      loading = false;
    }
  });

  function selectEntry(entry: StorageInventoryEntry) {
    const part: ScanPart = {
      id: entry.part_id,
      mpn: entry.mpn,
      description: entry.description,
      inventory_entry_id: entry.inventory_entry_id,
      quantity_on_hand: entry.quantity_on_hand,
      storage_location: entry.storage_location,
    };
    onPartSelect(part);
  }
</script>

<div class="flex flex-1 flex-col min-h-0">
  <!-- Header -->
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800 shrink-0">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <div class="flex-1 min-w-0">
      <p class="font-medium truncate">{breadcrumb || '—'}</p>
      {#if coordinate && coordinate.length > 0}
        <p class="text-xs text-zinc-500 truncate">Cell: {coordLabel(coordinate)}</p>
      {/if}
    </div>
  </div>

  <!-- Readonly grid visualiser for coordinate context -->
  {#if coordinate && coordinate.length > 0 && gridConfig}
    <div class="px-4 py-3 border-b border-zinc-800 bg-zinc-900 shrink-0">
      <MobileGridVisualiser
        config={gridConfig}
        address={gridAddress}
        selected={gridSelected}
        readonly={true}
      />
    </div>
  {/if}

  <!-- Search -->
  <div class="px-4 py-2 border-b border-zinc-800 shrink-0">
    <input
      type="search"
      placeholder="Search by MPN or description…"
      bind:value={searchQuery}
      class="w-full rounded-lg bg-zinc-800 px-3 py-2 text-sm placeholder-zinc-500 outline-none focus:ring-1 focus:ring-zinc-600"
    />
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-y-auto">
    {#if loading}
      <div class="flex flex-1 items-center justify-center py-16">
        <div class="h-6 w-6 animate-spin rounded-full border-2 border-zinc-600 border-t-white"></div>
      </div>
    {:else if filteredEntries.length === 0}
      <div class="flex flex-col items-center justify-center py-16 px-8 text-center gap-2">
        <svg class="h-8 w-8 text-zinc-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
            d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z" />
        </svg>
        <p class="text-sm text-zinc-500">
          {searchQuery ? 'No parts match your search' : 'No parts assigned to this location'}
        </p>
      </div>
    {:else}
      {#each filteredEntries as entry (entry.inventory_entry_id)}
        <button
          onclick={() => selectEntry(entry)}
          class="flex w-full items-center gap-3 px-4 py-3 text-left border-b border-zinc-800 active:bg-zinc-800"
        >
          <div class="flex-1 min-w-0">
            <p class="font-mono text-sm font-medium truncate">{entry.mpn ?? '—'}</p>
            {#if entry.description}
              <p class="text-xs text-zinc-400 truncate">{entry.description}</p>
            {/if}
            {#if entry.storage_location?.coordinate}
              <p class="text-xs text-zinc-600 font-mono">{coordLabel(entry.storage_location.coordinate)}</p>
            {/if}
          </div>
          <div class="text-right shrink-0">
            <p class="text-sm font-semibold tabular-nums">{entry.quantity_on_hand}</p>
            <p class="text-xs text-zinc-500">in stock</p>
          </div>
        </button>
      {/each}
    {/if}
  </div>
</div>

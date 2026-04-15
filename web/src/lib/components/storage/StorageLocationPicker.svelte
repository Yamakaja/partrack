<script lang="ts">
  import { Popover } from 'bits-ui';
  import { ChevronDown, ChevronRight, Check, Grid3x3, ArrowLeft } from 'lucide-svelte';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { cn } from '$lib/utils/cn.js';
  import GridVisual from './GridVisual.svelte';

  export interface StorageLocationValue {
    locationId: string;
    coordinate: number[] | null;
  }

  interface Props {
    value: StorageLocationValue | null;
    locations: StorageLocation[];
    onSelect: (v: StorageLocationValue | null) => void;
    placeholder?: string;
    disabled?: boolean;
  }

  let {
    value,
    locations,
    onSelect,
    placeholder = 'Select location…',
    disabled = false,
  }: Props = $props();

  let open = $state(false);
  let search = $state('');
  /** When set, we're in grid-picker mode for this location. */
  let gridPickerLocation = $state<StorageLocation | null>(null);
  /** The partial address built up during grid-picker step. */
  let gridAddress = $state<number[][]>([]);

  function handleOpenChange(v: boolean) {
    if (!v) {
      search = '';
      gridPickerLocation = null;
      gridAddress = [];
    }
    open = v;
  }

  // ── Tree helpers ──────────────────────────────────────────────────────────

  const locById = $derived.by(() => {
    const m = new Map<string, StorageLocation>();
    for (const l of locations) m.set(l.id, l);
    return m;
  });

  const childrenOf = $derived.by(() => {
    const m = new Map<string | null, StorageLocation[]>();
    for (const l of locations) {
      const key = l.parent_id;
      if (!m.has(key)) m.set(key, []);
      m.get(key)!.push(l);
    }
    for (const [, children] of m) {
      children.sort((a, b) => a.label.localeCompare(b.label));
    }
    return m;
  });

  /** Full label from root to a location, e.g. "East Wall / Organizer 2". */
  function fullPathLabel(locId: string): string {
    const parts: string[] = [];
    let cur = locById.get(locId);
    while (cur) {
      parts.unshift(cur.label);
      cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
    }
    return parts.join(' / ');
  }

  /** Label shown in the trigger button for the current value. */
  const selectedLabel = $derived.by(() => {
    if (!value) return null;
    const path = fullPathLabel(value.locationId);
    if (!value.coordinate || value.coordinate.length === 0) return path;
    // Append coordinate label
    const loc = locById.get(value.locationId);
    if (!loc?.grid_config) return path;
    const coordLabel = coordinateLabel(loc, value.coordinate);
    return coordLabel ? `${path} / ${coordLabel}` : path;
  });

  /** Convert a flat coordinate to a human-readable label given the location's grid_config. */
  function coordinateLabel(loc: StorageLocation, coord: number[]): string {
    if (!loc.grid_config || coord.length === 0) return '';
    const display = loc.grid_config.display ?? 'alphanumeric';
    const parts: string[] = [];
    let offset = 0;
    for (const level of loc.grid_config.levels) {
      const dimCount = level.dims.length;
      if (offset >= coord.length) break;
      const levelCoords = coord.slice(offset, offset + dimCount);
      offset += dimCount;
      if (level.dims.length === 1) {
        const row = levelCoords[0];
        parts.push(display === 'alphanumeric' ? String.fromCharCode(65 + row) : String(row + 1));
      } else {
        const row = levelCoords[0];
        const col = levelCoords[1] ?? 0;
        parts.push(display === 'alphanumeric'
          ? `${String.fromCharCode(65 + row)}${col + 1}`
          : `(${row + 1}, ${col + 1})`);
      }
    }
    return parts.join(' / ');
  }

  // ── Expand/collapse state ─────────────────────────────────────────────────

  let expanded = $state<Set<string>>(new Set());
  $effect(() => {
    // Auto-expand root nodes
    const roots = childrenOf.get(null) ?? [];
    expanded = new Set(roots.map((l) => l.id));
  });

  function toggleExpand(id: string, e: MouseEvent) {
    e.stopPropagation();
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }

  // ── Search ────────────────────────────────────────────────────────────────

  const trimmed = $derived(search.trim().toLowerCase());

  const searchResults = $derived.by(() => {
    if (!trimmed) return null;
    return locations.filter((l) =>
      l.label.toLowerCase().includes(trimmed) ||
      fullPathLabel(l.id).toLowerCase().includes(trimmed),
    ).sort((a, b) => a.label.localeCompare(b.label));
  });

  // ── Selection logic ───────────────────────────────────────────────────────

  function selectLocation(loc: StorageLocation) {
    if (loc.grid_config) {
      // Visual node → switch to grid picker
      gridPickerLocation = loc;
      gridAddress = [];
    } else {
      // Named node → commit immediately
      onSelect({ locationId: loc.id, coordinate: null });
      open = false;
    }
  }

  function commitGridSelection(coord: number[] | null) {
    if (!gridPickerLocation) return;
    onSelect({ locationId: gridPickerLocation.id, coordinate: coord });
    open = false;
  }

  function backToTree() {
    gridPickerLocation = null;
    gridAddress = [];
  }

  // ── Flat tree rendering helpers ───────────────────────────────────────────

  interface FlatNode { loc: StorageLocation; depth: number }

  /** Build a flat pre-order traversal respecting the expanded set. */
  const flatTree = $derived.by((): FlatNode[] => {
    const result: FlatNode[] = [];
    function visit(parentId: string | null, depth: number) {
      const children = childrenOf.get(parentId) ?? [];
      for (const loc of children) {
        result.push({ loc, depth });
        if (expanded.has(loc.id)) visit(loc.id, depth + 1);
      }
    }
    visit(null, 0);
    return result;
  });
</script>

<Popover.Root {open} onOpenChange={handleOpenChange}>
  <Popover.Trigger {disabled} class="w-full">
    <button
      type="button"
      {disabled}
      class={cn(
        'flex h-9 w-full items-center justify-between rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm',
        'hover:bg-accent/50 transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        'disabled:opacity-50 disabled:pointer-events-none',
        !selectedLabel && 'text-muted-foreground',
      )}
    >
      <span class="truncate">{selectedLabel ?? placeholder}</span>
      <ChevronDown class="h-4 w-4 flex-shrink-0 text-muted-foreground ml-2" />
    </button>
  </Popover.Trigger>

  <Popover.Content
    align="start"
    sideOffset={4}
    class="z-50 w-80 rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    {#if gridPickerLocation}
      <!-- Step 2: Grid picker -->
      <div class="border-b border-border px-3 py-2 flex items-center gap-2">
        <button
          type="button"
          class="text-muted-foreground hover:text-foreground transition-colors"
          onclick={backToTree}
          aria-label="Back to tree"
        >
          <ArrowLeft class="h-4 w-4" />
        </button>
        <span class="text-sm font-medium truncate">{fullPathLabel(gridPickerLocation.id)}</span>
      </div>
      <div class="p-3 flex flex-col gap-3">
        <GridVisual
          config={gridPickerLocation.grid_config!}
          address={gridAddress}
          onSelect={(addr) => { gridAddress = addr; }}
        />
        <!-- Commit buttons -->
        <div class="flex flex-col gap-1 pt-1 border-t border-border">
          {#if gridAddress.length > 0}
            <button
              type="button"
              class="w-full text-left px-2 py-1.5 text-sm rounded hover:bg-accent transition-colors"
              onclick={() => commitGridSelection(gridAddress.flat())}
            >
              Use: {coordinateLabel(gridPickerLocation, gridAddress.flat())}
            </button>
          {/if}
          <button
            type="button"
            class="w-full text-left px-2 py-1.5 text-sm text-muted-foreground rounded hover:bg-accent transition-colors"
            onclick={() => commitGridSelection(null)}
          >
            Use container only (no specific cell)
          </button>
        </div>
      </div>
    {:else}
      <!-- Step 1: Tree picker -->
      <div class="border-b border-border px-2 py-1.5">
        <input
          type="search"
          bind:value={search}
          placeholder="Search…"
          class="w-full bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
        />
      </div>

      <div class="max-h-72 overflow-y-auto py-1">
        <!-- None option -->
        <button
          type="button"
          class={cn(
            'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left',
            value == null && 'bg-accent/60',
          )}
          onclick={() => { onSelect(null); open = false; }}
        >
          <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
            {#if value == null}
              <Check class="h-3.5 w-3.5" />
            {/if}
          </span>
          <span class="text-muted-foreground italic">None</span>
        </button>

        {#if searchResults}
          <!-- Flat search results -->
          {#if searchResults.length === 0}
            <p class="px-4 py-2 text-sm text-muted-foreground">No results</p>
          {:else}
            {#each searchResults as loc (loc.id)}
              <button
                type="button"
                class={cn(
                  'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left',
                  value?.locationId === loc.id && 'bg-accent font-medium',
                )}
                onclick={() => selectLocation(loc)}
              >
                <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
                  {#if value?.locationId === loc.id && !loc.grid_config}
                    <Check class="h-3.5 w-3.5" />
                  {:else if loc.grid_config}
                    <Grid3x3 class="h-3.5 w-3.5 text-muted-foreground" />
                  {/if}
                </span>
                <span class="truncate">{fullPathLabel(loc.id)}</span>
              </button>
            {/each}
          {/if}
        {:else}
          <!-- Hierarchical tree -->
          {#each flatTree as { loc, depth } (loc.id)}
            {@const children = childrenOf.get(loc.id) ?? []}
            {@const isExpanded = expanded.has(loc.id)}
            {@const isSelected = value?.locationId === loc.id}
            <div class="flex w-full items-center">
              <button
                type="button"
                style="padding-left: {0.5 + depth * 1.25}rem"
                class={cn(
                  'flex flex-1 items-center gap-1.5 rounded-sm pr-2 py-1.5 text-sm hover:bg-accent text-left min-w-0',
                  isSelected && 'bg-accent font-medium',
                )}
                onclick={() => selectLocation(loc)}
              >
                <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
                  {#if isSelected && !loc.grid_config}
                    <Check class="h-3.5 w-3.5" />
                  {:else if loc.grid_config}
                    <Grid3x3 class="h-3.5 w-3.5 text-muted-foreground" />
                  {/if}
                </span>
                <span class="truncate">{loc.label}</span>
              </button>
              {#if children.length > 0 && !loc.grid_config}
                <button
                  type="button"
                  class="px-1.5 py-1.5 text-muted-foreground hover:text-foreground flex-shrink-0"
                  onclick={(e) => toggleExpand(loc.id, e)}
                  aria-label={isExpanded ? 'Collapse' : 'Expand'}
                >
                  {#if isExpanded}
                    <ChevronDown class="h-3.5 w-3.5" />
                  {:else}
                    <ChevronRight class="h-3.5 w-3.5" />
                  {/if}
                </button>
              {/if}
            </div>
          {/each}
        {/if}
      </div>
    {/if}
  </Popover.Content>
</Popover.Root>

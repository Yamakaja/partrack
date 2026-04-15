<script lang="ts">
  import { Popover } from 'bits-ui';
  import { ChevronDown, ChevronRight, Check, Grid3x3 } from 'lucide-svelte';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { cn } from '$lib/utils/cn.js';
  import GridVisual from './GridVisual.svelte';

  export interface StorageLocationFilter {
    locationId: string;
    /** Full flat coordinate per selected cell. null = entire location (any cell). */
    cells: number[][] | null;
  }

  interface Props {
    value: StorageLocationFilter | null;
    locations: StorageLocation[];
    onSelect: (v: StorageLocationFilter | null) => void;
    placeholder?: string;
  }

  let {
    value,
    locations,
    onSelect,
    placeholder = 'All locations',
  }: Props = $props();

  let open = $state(false);
  let search = $state('');
  let activeGridId = $state<string | null>(null);
  let gridAddress = $state<number[][]>([]);
  /** Full flat coordinates of currently selected cells in the active grid. */
  let selectedCells = $state<number[][]>([]);

  function resetState() {
    search = '';
    activeGridId = null;
    gridAddress = [];
    selectedCells = [];
  }

  function handleOpenChange(v: boolean) {
    if (!v) {
      resetState();
    } else if (value) {
      const loc = locById.get(value.locationId);
      if (loc?.grid_config) {
        activeGridId = value.locationId;
        selectedCells = value.cells ?? [];
        gridAddress = [];
        // Expand ancestors so the selected location is visible
      }
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

  function fullPathLabel(locId: string): string {
    const parts: string[] = [];
    let cur = locById.get(locId);
    while (cur) {
      parts.unshift(cur.label);
      cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
    }
    return parts.join(' > ');
  }

  /** Label shown on the trigger button. */
  const triggerLabel = $derived.by(() => {
    if (!value) return null;
    const path = fullPathLabel(value.locationId);
    if (!value.cells || value.cells.length === 0) return path;
    return `${path} (${value.cells.length} cell${value.cells.length === 1 ? '' : 's'})`;
  });

  // ── Expand/collapse ───────────────────────────────────────────────────────

  let expanded = $state<Set<string>>(new Set());
  $effect(() => {
    const roots = childrenOf.get(null) ?? [];
    const next = new Set(roots.map((l) => l.id));
    if (value) {
      let cur = locById.get(value.locationId);
      while (cur?.parent_id) {
        next.add(cur.parent_id);
        cur = locById.get(cur.parent_id);
      }
    }
    expanded = next;
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
    return locations
      .filter(
        (l) =>
          l.label.toLowerCase().includes(trimmed) ||
          fullPathLabel(l.id).toLowerCase().includes(trimmed),
      )
      .sort((a, b) => a.label.localeCompare(b.label));
  });

  // ── Active grid location ──────────────────────────────────────────────────

  const activeGridLoc = $derived(activeGridId ? locById.get(activeGridId) : null);

  /**
   * Selected cells relative to the current gridAddress level — what GridVisual expects.
   * Full stored coords are stripped of the address prefix.
   */
  const relativeSelected = $derived.by((): number[][] => {
    const prefix = gridAddress.flat();
    const prefixKey = prefix.join(',');
    return selectedCells
      .filter((c) => {
        if (prefix.length === 0) return true;
        return c.join(',').startsWith(prefixKey + (c.length > prefix.length ? ',' : ''));
      })
      .map((c) => c.slice(prefix.length));
  });

  // ── Selection logic ───────────────────────────────────────────────────────

  function selectLocation(loc: StorageLocation) {
    if (loc.grid_config) {
      if (activeGridId !== loc.id) {
        gridAddress = [];
        selectedCells = [];
      }
      activeGridId = loc.id;
    } else {
      onSelect({ locationId: loc.id, cells: null });
      resetState();
      open = false;
    }
  }

  function applyCurrentSelection() {
    if (!activeGridId) return;
    onSelect({ locationId: activeGridId, cells: selectedCells.length > 0 ? selectedCells : null });
  }

  function handleDrillDown(addr: number[][]) {
    gridAddress = addr;
  }

  function toggleCell(coord: number[]) {
    const full = [...gridAddress.flat(), ...coord];
    const key = full.join(',');
    const exists = selectedCells.some((c) => c.join(',') === key);
    selectedCells = exists
      ? selectedCells.filter((c) => c.join(',') !== key)
      : [...selectedCells, full];
    applyCurrentSelection();
  }

  function addDragRect(cells: number[][], mode: 'add' | 'remove') {
    const fullCells = cells.map((c) => [...gridAddress.flat(), ...c]);
    const keys = new Set(fullCells.map((c) => c.join(',')));
    if (mode === 'add') {
      const existing = new Set(selectedCells.map((c) => c.join(',')));
      selectedCells = [
        ...selectedCells,
        ...fullCells.filter((c) => !existing.has(c.join(','))),
      ];
    } else {
      selectedCells = selectedCells.filter((c) => !keys.has(c.join(',')));
    }
    applyCurrentSelection();
  }

  // ── Flat tree ─────────────────────────────────────────────────────────────

  interface FlatNode {
    loc: StorageLocation;
    depth: number;
  }

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
  <Popover.Trigger>
    <button
      type="button"
      class={cn(
        'flex h-9 items-center justify-between rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm',
        'hover:bg-accent/50 transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        !triggerLabel && 'text-muted-foreground',
      )}
    >
      <span class="truncate max-w-48">{triggerLabel ?? placeholder}</span>
      <ChevronDown class="h-4 w-4 flex-shrink-0 text-muted-foreground ml-2" />
    </button>
  </Popover.Trigger>

  <Popover.Content
    align="start"
    sideOffset={4}
    class="z-50 flex overflow-hidden rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    <!-- Left panel: tree -->
    <div class="flex w-52 flex-col border-r border-border">
      <!-- Search -->
      <div class="border-b border-border px-2 py-1.5">
        <input
          type="search"
          bind:value={search}
          placeholder="Search…"
          class="w-full bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
        />
      </div>

      <div class="max-h-72 overflow-y-auto py-1">
        <!-- None / clear option -->
        <button
          type="button"
          class={cn(
            'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left',
            value == null && 'bg-accent/60',
          )}
          onclick={() => { onSelect(null); resetState(); open = false; }}
        >
          <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
            {#if value == null}
              <Check class="h-3.5 w-3.5" />
            {/if}
          </span>
          <span class="text-muted-foreground italic">All locations</span>
        </button>

        {#if searchResults}
          {#if searchResults.length === 0}
            <p class="px-4 py-2 text-sm text-muted-foreground">No results</p>
          {:else}
            {#each searchResults as loc (loc.id)}
              {@const isActive = !!loc.grid_config && activeGridId === loc.id}
              {@const isSelected = !activeGridId && value?.locationId === loc.id}
              <button
                type="button"
                class={cn(
                  'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left',
                  (isSelected || isActive) && 'bg-accent font-medium',
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
                <span class="truncate">{fullPathLabel(loc.id)}</span>
              </button>
            {/each}
          {/if}
        {:else}
          <!-- Hierarchical tree -->
          {#each flatTree as { loc, depth } (loc.id)}
            {@const children = childrenOf.get(loc.id) ?? []}
            {@const isExpanded = expanded.has(loc.id)}
            {@const isSelected = !activeGridId && value?.locationId === loc.id}
            {@const isActive = !!loc.grid_config && activeGridId === loc.id}
            <div class="flex w-full items-center">
              <button
                type="button"
                style="padding-left: {0.5 + depth * 1.25}rem"
                class={cn(
                  'flex flex-1 items-center gap-1.5 rounded-sm pr-2 py-1.5 text-sm hover:bg-accent text-left min-w-0',
                  (isSelected || isActive) && 'bg-accent font-medium',
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
    </div>

    <!-- Right panel: multiselect grid or placeholder -->
    <div class="flex w-72 flex-col">
      {#if activeGridLoc}
        <div class="border-b border-border px-3 py-2 text-sm font-medium truncate">
          {fullPathLabel(activeGridLoc.id)}
        </div>

        <div class="flex-1 overflow-auto p-3">
          <GridVisual
            config={activeGridLoc.grid_config!}
            address={gridAddress}
            onSelect={handleDrillDown}
            multiSelect={true}
            selectedCells={relativeSelected}
            onCellToggle={toggleCell}
            onDragSelect={addDragRect}
          />
        </div>

        <div class="border-t border-border p-2">
          <button
            type="button"
            class="w-full text-left px-2 py-1.5 text-sm text-muted-foreground rounded hover:bg-accent transition-colors"
            onclick={() => { selectedCells = []; applyCurrentSelection(); }}
          >
            Use all cells in this location
          </button>
        </div>
      {:else}
        <div class="flex flex-1 items-center justify-center p-6 text-center text-sm text-muted-foreground">
          Select a grid location to filter by specific cells
        </div>
      {/if}
    </div>
  </Popover.Content>
</Popover.Root>

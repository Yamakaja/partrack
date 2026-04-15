<script lang="ts">
  import { untrack } from 'svelte';
  import type { GridConfig } from '$lib/types/inventory.js';

  interface Props {
    config: GridConfig;
    /** Current selection as array of per-level coordinate tuples (partial). */
    address: number[][];
    /** Flat coordinates that have inventory (matched against address prefix). */
    occupied?: number[][];
    onSelect: (newAddress: number[][]) => void;
    /**
     * Enable multi-select at the leaf level. When true, clicking leaf-level cells
     * toggles them and drag-to-select draws a rectangular region.
     */
    multiSelect?: boolean;
    /** Which leaf-level cells are currently selected (passed from parent). */
    selectedCells?: number[][];
    /** Called when a single leaf-level cell is clicked (toggle). */
    onCellToggle?: (coord: number[]) => void;
    /** Called when a rectangular drag completes, with all cells in the rectangle. */
    onDragSelect?: (cells: number[][], mode: 'add' | 'remove') => void;
  }

  let {
    config,
    address,
    occupied = [],
    onSelect,
    multiSelect = false,
    selectedCells = [],
    onCellToggle,
    onDragSelect,
  }: Props = $props();

  let currentLevelIndex = $derived(address.length);
  let currentLevel = $derived(config.levels[currentLevelIndex] ?? null);

  const display = $derived(config.display ?? 'alphanumeric');

  /** True when we are rendering the deepest (leaf) level of the grid. */
  const isLeafLevel = $derived(currentLevelIndex === config.levels.length - 1);
  /** Whether to use multi-select behaviour for this rendered level. */
  const useMultiSelect = $derived(multiSelect && isLeafLevel);

  // ── Rectangular drag state ────────────────────────────────────────────────
  let dragStart = $state<{ row: number; col?: number } | null>(null);
  let dragCurrent = $state<{ row: number; col?: number } | null>(null);
  let dragMode = $state<'add' | 'remove'>('add');
  const isDragging = $derived(dragStart !== null);

  /**
   * All cells in the bounding rectangle from dragStart to dragCurrent.
   * Empty when not dragging or no movement yet.
   */
  const draftRect = $derived.by((): number[][] => {
    if (!dragStart || !dragCurrent || !currentLevel) return [];
    const minRow = Math.min(dragStart.row, dragCurrent.row);
    const maxRow = Math.max(dragStart.row, dragCurrent.row);
    if (currentLevel.dims.length === 1) {
      // 1D: a contiguous range
      const cells: number[][] = [];
      for (let r = minRow; r <= maxRow; r++) cells.push([r]);
      return cells;
    } else {
      // 2D: a rectangle
      const minCol = Math.min(dragStart.col ?? 0, dragCurrent.col ?? 0);
      const maxCol = Math.max(dragStart.col ?? 0, dragCurrent.col ?? 0);
      const cells: number[][] = [];
      for (let r = minRow; r <= maxRow; r++) {
        for (let c = minCol; c <= maxCol; c++) cells.push([r, c]);
      }
      return cells;
    }
  });

  /** Set for O(1) draft-membership lookup in the template. */
  const draftSet = $derived.by(() => {
    const s = new Set<string>();
    for (const c of draftRect) s.add(c.join(','));
    return s;
  });

  /**
   * Commit the current drag: single-click → toggle; rectangle drag → batch add.
   * Safe to call even when not dragging (returns early).
   */
  function commitDrag() {
    if (!dragStart) return;
    const cells = draftRect;   // capture before clearing state
    const start = dragStart;   // capture before clearing
    dragStart = null;
    dragCurrent = null;

    if (cells.length <= 1) {
      // Click (no movement or single cell) — toggle
      const coord = start.col !== undefined ? [start.row, start.col] : [start.row];
      onCellToggle?.(coord);
    } else {
      // Rectangle drag — add or remove based on starting cell
      onDragSelect?.(cells, dragMode);
    }
  }

  // Commit the drag on any pointerup anywhere on the page.
  $effect(() => {
    function handleGlobalPointerUp() { untrack(commitDrag); }
    document.addEventListener('pointerup', handleGlobalPointerUp);
    return () => document.removeEventListener('pointerup', handleGlobalPointerUp);
  });

  // ── Multi-select helpers ──────────────────────────────────────────────────
  function isCellSelectedMulti(row: number, col?: number): boolean {
    if (!selectedCells || selectedCells.length === 0) return false;
    return selectedCells.some((c) =>
      col !== undefined
        ? c.length === 2 && c[0] === row && c[1] === col
        : c.length === 1 && c[0] === row,
    );
  }

  // ── Navigation helpers (non-multi-select mode) ────────────────────────────
  /** Convert a flat 0-based coordinate within the current level to its display label. */
  function cellLabel(levelDims: number[], row: number, col?: number): string {
    if (levelDims.length === 1) {
      if (display === 'alphanumeric') return String.fromCharCode(65 + row);
      return String(row + 1);
    }
    if (display === 'alphanumeric') {
      return `${String.fromCharCode(65 + row)}${(col ?? 0) + 1}`;
    }
    return `(${row + 1}, ${(col ?? 0) + 1})`;
  }

  function isCellOccupied(levelDims: number[], row: number, col?: number): boolean {
    const cellCoords = levelDims.length === 1 ? [row] : [row, col ?? 0];
    const prefix = [...address.flat(), ...cellCoords];
    return occupied.some((occ) => {
      if (occ.length < prefix.length) return false;
      return prefix.every((v, i) => occ[i] === v);
    });
  }

  function isCellSelected(levelDims: number[], row: number, col?: number): boolean {
    if (address.length <= currentLevelIndex) return false;
    const nextEntry = address[currentLevelIndex];
    if (levelDims.length === 1) return nextEntry?.[0] === row;
    return nextEntry?.[0] === row && nextEntry?.[1] === (col ?? 0);
  }

  function handleCellClick(levelDims: number[], row: number, col?: number) {
    const coords = levelDims.length === 1 ? [row] : [row, col ?? 0];
    const newAddress = [...address.slice(0, currentLevelIndex), coords];
    onSelect(newAddress);
  }

  function levelBreadcrumb(levelIdx: number): string {
    if (levelIdx >= address.length) return '';
    const lvl = config.levels[levelIdx];
    if (!lvl) return '';
    const coords = address[levelIdx];
    if (lvl.dims.length === 1) return cellLabel(lvl.dims, coords[0]);
    return cellLabel(lvl.dims, coords[0], coords[1]);
  }
</script>

<div class="flex flex-col gap-3">
  <!-- Breadcrumb showing addressed levels -->
  {#if address.length > 0}
    <div class="flex items-center gap-1 text-sm text-muted-foreground">
      <button class="hover:text-foreground transition-colors" onclick={() => onSelect([])}>
        All
      </button>
      {#each address as _, i}
        <span>/</span>
        <button
          class="hover:text-foreground transition-colors {i === address.length - 1 ? 'text-foreground font-medium' : ''}"
          onclick={() => onSelect(address.slice(0, i + 1))}
        >
          {#if config.levels[i]?.name}
            {config.levels[i].name}
            {levelBreadcrumb(i)}
          {:else}
            {levelBreadcrumb(i)}
          {/if}
        </button>
      {/each}
    </div>
  {/if}

  {#if currentLevel}
    {@const dims = currentLevel.dims}
    {@const rows = dims[0]}
    {@const cols = dims.length > 1 ? dims[1] : 1}
    {@const is1D = dims.length === 1}

    {#if currentLevel.name}
      <p class="text-xs text-muted-foreground uppercase tracking-wide font-semibold">
        {currentLevel.name}
      </p>
    {/if}

    {#if is1D}
      <!-- 1D: horizontal strip -->
      <div class="flex flex-wrap gap-1.5">
        {#each { length: rows } as _, row}
          {@const occupied_ = isCellOccupied(dims, row)}
          {@const selected = useMultiSelect ? isCellSelectedMulti(row) : isCellSelected(dims, row)}
          {@const inDraft = useMultiSelect && isDragging && draftSet.has(String(row))}
          {@const inAddDraft = inDraft && dragMode === 'add'}
          {@const inRemoveDraft = inDraft && dragMode === 'remove' && selected}
          <button
            class="w-10 h-10 rounded border text-xs font-mono transition-colors select-none
              {inRemoveDraft
                ? 'bg-primary/20 border-primary/30 text-muted-foreground'
                : selected
                  ? 'bg-primary text-primary-foreground border-primary'
                  : inAddDraft
                    ? 'bg-primary/40 border-primary/60'
                    : occupied_
                      ? 'bg-accent border-accent-foreground/30 hover:bg-accent/80'
                      : 'border-border hover:bg-muted'}"
            onpointerdown={useMultiSelect
              ? (e: PointerEvent) => { e.preventDefault(); dragMode = isCellSelectedMulti(row) ? 'remove' : 'add'; dragStart = { row }; dragCurrent = { row }; }
              : undefined}
            onpointerenter={useMultiSelect
              ? () => { if (dragStart) dragCurrent = { row }; }
              : undefined}
            onclick={!useMultiSelect ? () => handleCellClick(dims, row) : undefined}
            title={cellLabel(dims, row)}
          >
            {cellLabel(dims, row)}
          </button>
        {/each}
      </div>
    {:else}
      <!-- 2D: grid -->
      <div
        class="grid gap-1"
        style="grid-template-columns: repeat({cols}, minmax(0, 2.5rem));"
      >
        {#each { length: rows } as _, row}
          {#each { length: cols } as _, col}
            {@const occupied_ = isCellOccupied(dims, row, col)}
            {@const selected = useMultiSelect ? isCellSelectedMulti(row, col) : isCellSelected(dims, row, col)}
            {@const inDraft = useMultiSelect && isDragging && draftSet.has(`${row},${col}`)}
            {@const inAddDraft = inDraft && dragMode === 'add'}
            {@const inRemoveDraft = inDraft && dragMode === 'remove' && selected}
            <button
              class="h-9 rounded border text-xs font-mono transition-colors select-none
                {inRemoveDraft
                  ? 'bg-primary/20 border-primary/30 text-muted-foreground'
                  : selected
                    ? 'bg-primary text-primary-foreground border-primary'
                    : inAddDraft
                      ? 'bg-primary/40 border-primary/60'
                      : occupied_
                        ? 'bg-accent border-accent-foreground/30 hover:bg-accent/80'
                        : 'border-border hover:bg-muted'}"
              onpointerdown={useMultiSelect
                ? (e: PointerEvent) => { e.preventDefault(); dragMode = isCellSelectedMulti(row, col) ? 'remove' : 'add'; dragStart = { row, col }; dragCurrent = { row, col }; }
                : undefined}
              onpointerenter={useMultiSelect
                ? () => { if (dragStart) dragCurrent = { row, col }; }
                : undefined}
              onclick={!useMultiSelect ? () => handleCellClick(dims, row, col) : undefined}
              title={cellLabel(dims, row, col)}
            >
              {cellLabel(dims, row, col)}
            </button>
          {/each}
        {/each}
      </div>
    {/if}

    {#if currentLevelIndex < config.levels.length - 1}
      <p class="text-xs text-muted-foreground">
        Select a cell to drill down to the next level.
      </p>
    {/if}
  {:else if config.levels.length > 0 && address.length >= config.levels.length}
    <p class="text-sm text-muted-foreground">Fully addressed.</p>
  {/if}
</div>

<script lang="ts">
  import type { GridConfig } from '$lib/types/inventory.js';
  import GridLevelCells from '$lib/components/storage/GridLevelCells.svelte';

  interface Props {
    config: GridConfig;
    /** Selected address per level (each element is [row] or [row, col]) for parent levels. */
    address: number[][];
    /** Currently selected coordinate at the displayed level. */
    selected: number[] | null;
    /** Cells known to be occupied — serialised as coordinate.join(',') */
    occupied?: Set<string>;
    readonly?: boolean;
    onCellTap?: (coord: number[]) => void;
  }

  let {
    config,
    address,
    selected = null,
    occupied,
    readonly = false,
    onCellTap,
  }: Props = $props();

  const display = $derived(config.display ?? 'alphanumeric');
  const levelIndex = $derived(address.length);
  const level = $derived(config.levels[levelIndex]);
  const parentLevels = $derived(config.levels.slice(0, levelIndex));

  // ── Helpers ──────────────────────────────────────────────────────────────

  function cellLabel1D(idx: number): string {
    return display === 'alphanumeric' ? String.fromCharCode(65 + idx) : String(idx + 1);
  }

  function cellLabel2D(row: number, col: number): string {
    return display === 'alphanumeric'
      ? `${String.fromCharCode(65 + row)}${col + 1}`
      : `(${row + 1},${col + 1})`;
  }

  function levelCellLabel(levelIdx: number, addr: number[]): string {
    const lv = config.levels[levelIdx];
    if (!lv) return '';
    const levelName = lv.name ?? `L${levelIdx + 1}`;
    if (lv.dims.length === 1) return `${levelName} ${cellLabel1D(addr[0])}`;
    return `${levelName} ${cellLabel2D(addr[0], addr[1])}`;
  }

  function coordKey(row: number, col?: number): string {
    const prefix = address.flatMap(a => a).join(',');
    const cell = col !== undefined ? `${row},${col}` : `${row}`;
    return prefix ? `${prefix},${cell}` : cell;
  }

  function isOccupied(row: number, col?: number): boolean {
    if (!occupied) return false;
    return occupied.has(coordKey(row, col));
  }

  function isSelected(row: number, col?: number): boolean {
    if (!selected) return false;
    if (col !== undefined) return selected[0] === row && selected[1] === col;
    return selected[0] === row;
  }

  // Rendering tier decision (based on max axis size of the current level)
  const maxAxis = $derived(level ? Math.max(...level.dims) : 0);
  const tier = $derived(maxAxis <= 8 ? 'cells' : maxAxis <= 32 ? 'dots' : 'text');

  // For dots tier: compute rows/cols of current level
  const rows = $derived(level?.dims[0] ?? 0);
  const cols = $derived(level?.dims.length > 1 ? level.dims[1] : 1);
  const is1D = $derived(!level || level.dims.length === 1);

  function handleTap(row: number, col?: number) {
    if (readonly) return;
    onCellTap?.(is1D ? [row] : [row, col ?? 0]);
  }

  function dotClass(row: number, col?: number): string {
    const sel = isSelected(row, col);
    const occ = isOccupied(row, col);
    if (sel) return 'bg-white w-3 h-3';
    if (occ) return 'bg-zinc-500 w-2 h-2';
    return 'bg-zinc-700 w-2 h-2';
  }

  const selectedLabel = $derived(() => {
    if (!selected) return null;
    if (!level) return null;
    if (is1D) return cellLabel1D(selected[0]);
    return cellLabel2D(selected[0], selected[1] ?? 0);
  });
</script>

<div class="space-y-2">
  <!-- Breadcrumb for parent levels -->
  {#if parentLevels.length > 0}
    <p class="text-xs text-zinc-400 truncate">
      {#each parentLevels as _lv, i}
        {#if i > 0}<span class="mx-1 text-zinc-600">→</span>{/if}
        <span>{levelCellLabel(i, address[i])}</span>
      {/each}
    </p>
  {/if}

  {#if !level}
    <p class="text-xs text-zinc-500">No grid at this level.</p>
  {:else if tier === 'cells'}
    <!-- ≤ 8 per axis: use the existing labelled cell component -->
    <GridLevelCells
      {level}
      {display}
      selected={selected}
      isOccupied={is1D ? (r) => isOccupied(r) : (r, c) => isOccupied(r, c)}
      {readonly}
      onSelect={(coord) => onCellTap?.(coord)}
    />
  {:else if tier === 'dots'}
    <!-- 9–32 per axis: dot matrix -->
    {#if is1D}
      <div class="flex flex-wrap gap-2">
        {#each { length: rows } as _, row}
          <button
            type="button"
            disabled={readonly}
            aria-label={cellLabel1D(row)}
            onclick={() => handleTap(row)}
            class="flex items-center justify-center w-6 h-6 rounded-full transition-all disabled:cursor-default"
          >
            <span class="rounded-full transition-all {dotClass(row)}"></span>
          </button>
        {/each}
      </div>
    {:else}
      <div class="grid gap-1.5" style="grid-template-columns: repeat({cols}, minmax(0, 1.5rem));">
        {#each { length: rows } as _, row}
          {#each { length: cols } as _, col}
            <button
              type="button"
              disabled={readonly}
              aria-label={cellLabel2D(row, col)}
              onclick={() => handleTap(row, col)}
              class="flex items-center justify-center w-6 h-6 rounded-full transition-all disabled:cursor-default"
            >
              <span class="rounded-full transition-all {dotClass(row, col)}"></span>
            </button>
          {/each}
        {/each}
      </div>
    {/if}
    {#if selected}
      <p class="text-xs text-zinc-400 text-center tabular-nums">{selectedLabel()}</p>
    {/if}
  {:else}
    <!-- > 32: coordinate text only -->
    <div class="flex items-center gap-2">
      <span class="text-xs text-zinc-500">Cell:</span>
      {#if selected}
        <span class="font-mono text-sm">{selectedLabel()}</span>
      {:else}
        <span class="text-sm text-zinc-600">—</span>
      {/if}
    </div>
  {/if}
</div>

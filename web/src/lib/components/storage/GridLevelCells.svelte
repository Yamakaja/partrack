<script lang="ts">
  import type { GridLevel } from '$lib/types/inventory.js';

  interface Props {
    level: GridLevel;
    display?: 'alphanumeric' | 'numeric';
    /** The selected cell at this level: [row] for 1D, [row, col] for 2D. */
    selected?: number[] | null;
    /** Returns true if the cell has inventory (used for occupied highlight). */
    isOccupied?: (row: number, col?: number) => boolean;
    readonly?: boolean;
    onSelect?: (coord: number[]) => void;
  }

  let {
    level,
    display = 'alphanumeric',
    selected = null,
    isOccupied,
    readonly = false,
    onSelect,
  }: Props = $props();

  const dims = $derived(level.dims);
  const rows = $derived(dims[0]);
  const cols = $derived(dims.length > 1 ? dims[1] : 1);
  const is1D = $derived(dims.length === 1);

  function cellLabel(row: number, col?: number): string {
    if (dims.length === 1) {
      return display === 'alphanumeric' ? String.fromCharCode(65 + row) : String(row + 1);
    }
    if (display === 'alphanumeric') return `${String.fromCharCode(65 + row)}${(col ?? 0) + 1}`;
    return `(${row + 1}, ${(col ?? 0) + 1})`;
  }

  function isSelected(row: number, col?: number): boolean {
    if (!selected) return false;
    if (dims.length === 1) return selected[0] === row;
    return selected[0] === row && selected[1] === (col ?? 0);
  }

  function handleClick(row: number, col?: number) {
    if (readonly) return;
    onSelect?.(dims.length === 1 ? [row] : [row, col ?? 0]);
  }

  function cellClass(sel: boolean, occ: boolean): string {
    if (sel) return 'bg-primary text-primary-foreground border-primary';
    if (occ) return 'bg-accent border-accent-foreground/30' + (readonly ? '' : ' hover:bg-accent/80');
    return 'border-border' + (readonly ? '' : ' hover:bg-muted');
  }
</script>

{#if is1D}
  <div class="flex flex-wrap gap-1.5">
    {#each { length: rows } as _, row}
      {@const sel = isSelected(row)}
      {@const occ = isOccupied?.(row) ?? false}
      <button
        type="button"
        disabled={readonly}
        class="w-10 h-10 rounded border text-xs font-mono transition-colors select-none disabled:cursor-default {cellClass(sel, occ)}"
        onclick={() => handleClick(row)}
        title={cellLabel(row)}
      >
        {cellLabel(row)}
      </button>
    {/each}
  </div>
{:else}
  <div class="grid gap-1" style="grid-template-columns: repeat({cols}, minmax(0, 2.5rem));">
    {#each { length: rows } as _, row}
      {#each { length: cols } as _, col}
        {@const sel = isSelected(row, col)}
        {@const occ = isOccupied?.(row, col) ?? false}
        <button
          type="button"
          disabled={readonly}
          class="h-9 rounded border text-xs font-mono transition-colors select-none disabled:cursor-default {cellClass(sel, occ)}"
          onclick={() => handleClick(row, col)}
          title={cellLabel(row, col)}
        >
          {cellLabel(row, col)}
        </button>
      {/each}
    {/each}
  </div>
{/if}

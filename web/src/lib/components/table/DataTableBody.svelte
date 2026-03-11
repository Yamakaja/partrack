<script lang="ts" generics="T">
  import { ChevronRight, ChevronDown } from 'lucide-svelte';
  import { TableBody, TableRow, TableCell } from '$lib/components/ui/table/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import { cn } from '$lib/utils/cn.js';
  import type { DataSource, ColumnSpec } from './types.js';
  import type { Component, Snippet } from 'svelte';

  let {
    source,
    getRowKey,
    onRowClick,
    rowClass,
    renderers,
    detailPanel,
    detailPanelOpen,
    actions,
    compact = false,
    emptyMessage = 'No items found',
    emptyIcon,
  }: {
    source: DataSource<T>;
    getRowKey: (row: T) => string;
    onRowClick?: (row: T) => void;
    rowClass?: (row: T) => string;
    renderers?: Record<string, Snippet<[value: unknown, row: T]>>;
    detailPanel?: Snippet<[row: T]>;
    detailPanelOpen?: (row: T) => boolean;
    actions?: Snippet<[row: T]>;
    compact?: boolean;
    emptyMessage?: string;
    emptyIcon?: Component;
  } = $props();

  // Track expanded rows
  let expanded = $state(new Map<string, boolean>());

  function isExpanded(row: T): boolean {
    const key = getRowKey(row);
    if (expanded.has(key)) return expanded.get(key)!;
    return detailPanelOpen?.(row) ?? false;
  }

  function toggleExpand(row: T): void {
    const key = getRowKey(row);
    const current = isExpanded(row);
    expanded.set(key, !current);
    expanded = new Map(expanded);
  }

  const visibleColumns = $derived(
    source.columns.filter((c) => !c.hidden && !c.exportOnly),
  );

  // Total colspan for empty/loading rows
  const colSpan = $derived(
    visibleColumns.length +
      (detailPanel ? 1 : 0) +
      (actions ? 1 : 0),
  );

  const cellPadding = $derived(compact ? 'py-2 px-4' : 'py-4 px-4');

  function getCellValue(row: T, key: string): unknown {
    if (source.getValue) return source.getValue(row, key);
    return getNestedValue(row, key);
  }

  function defaultCellValue(col: ColumnSpec<T>, row: T): string {
    const val = getCellValue(row, col.key);
    if (val === null || val === undefined) return '—';
    switch (col.type) {
      case 'numeric':
        return col.unit ? `${val} ${col.unit}` : String(val);
      case 'currency':
        return `$${parseFloat(String(val)).toFixed(2)}`;
      case 'date':
        return new Date(String(val)).toLocaleDateString();
      default:
        return String(val);
    }
  }

  function getNestedValue(obj: T, key: string): unknown {
    return key.split('.').reduce((acc: unknown, part) => {
      if (acc !== null && acc !== undefined && typeof acc === 'object') {
        return (acc as Record<string, unknown>)[part];
      }
      return undefined;
    }, obj as unknown);
  }

  const SKELETON_ROWS = 5;
</script>

<TableBody>
  {#if source.loading}
    {#each { length: SKELETON_ROWS } as _, i (i)}
      <TableRow>
        {#if detailPanel}
          <TableCell class={cn('w-8 px-2', cellPadding)}>
            <Skeleton class="h-4 w-4" />
          </TableCell>
        {/if}
        {#each visibleColumns as col (col.key)}
          <TableCell class={cellPadding}>
            <Skeleton class="h-4 w-full max-w-32" />
          </TableCell>
        {/each}
        {#if actions}
          <TableCell class={cellPadding}></TableCell>
        {/if}
      </TableRow>
    {/each}
  {:else if source.rows.length === 0}
    <TableRow>
      <TableCell colspan={colSpan} class="p-0">
        <EmptyState title={emptyMessage} />
      </TableCell>
    </TableRow>
  {:else}
    {#each source.rows as row (getRowKey(row))}
      {@const key = getRowKey(row)}
      {@const expanded_ = isExpanded(row)}
      <TableRow
        class={cn(
          onRowClick && 'cursor-pointer',
          rowClass?.(row),
        )}
        onclick={onRowClick ? () => onRowClick(row) : undefined}
      >
        {#if detailPanel}
          <TableCell class={cn('w-8 px-2', cellPadding)}>
            <button
              class="flex items-center justify-center rounded hover:bg-accent p-0.5 text-muted-foreground"
              onclick={(e) => { e.stopPropagation(); toggleExpand(row); }}
              aria-label={expanded_ ? 'Collapse' : 'Expand'}
            >
              {#if expanded_}
                <ChevronDown class="h-4 w-4" />
              {:else}
                <ChevronRight class="h-4 w-4" />
              {/if}
            </button>
          </TableCell>
        {/if}
        {#each visibleColumns as col (col.key)}
          <TableCell
            class={cn(
              cellPadding,
              (col.align === 'right' || col.type === 'numeric' || col.type === 'currency') &&
                'text-right tabular-nums',
            )}
          >
            {#if renderers?.[col.key]}
              {@render renderers[col.key](getCellValue(row, col.key), row)}
            {:else if col.render}
              {@render col.render(getCellValue(row, col.key), row)}
            {:else}
              {defaultCellValue(col, row)}
            {/if}
          </TableCell>
        {/each}
        {#if actions}
          <TableCell class={cn('text-right', cellPadding)}>
            {@render actions(row)}
          </TableCell>
        {/if}
      </TableRow>
      {#if detailPanel && expanded_}
        <TableRow class="hover:bg-transparent">
          <TableCell colspan={colSpan} class="p-0 border-b">
            {@render detailPanel(row)}
          </TableCell>
        </TableRow>
      {/if}
    {/each}
  {/if}
</TableBody>

<script lang="ts" generics="T">
  import { ArrowUp, ArrowDown, ArrowUpDown } from 'lucide-svelte';
  import DateRangeFilterPopover from './DateRangeFilterPopover.svelte';
  import NumericFilterPopover from './NumericFilterPopover.svelte';
  import ListFilterPopover from './ListFilterPopover.svelte';
  import { TableHeader, TableRow, TableHead } from '$lib/components/ui/table/index.js';
  import { cn } from '$lib/utils/cn.js';
  import type { DataSource, ColumnSpec } from './types.js';
  import type { Snippet } from 'svelte';

  let {
    source,
    actions,
    detailPanel,
    stickyHeader = true,
  }: {
    source: DataSource<T>;
    actions?: Snippet<[row: T]>;
    detailPanel?: Snippet<[row: T]>;
    stickyHeader?: boolean;
  } = $props();

  const visibleColumns = $derived(
    source.columns.filter((c) => !c.hidden && !c.exportOnly),
  );

  function nextSortDir(col: ColumnSpec<T>): 'asc' | 'desc' {
    if (source.sortKey === col.key) {
      return source.sortDirection === 'asc' ? 'desc' : 'asc';
    }
    return 'asc';
  }

  function ariaSort(col: ColumnSpec<T>): 'ascending' | 'descending' | 'none' {
    if (source.sortKey !== col.key) return 'none';
    return source.sortDirection === 'asc' ? 'ascending' : 'descending';
  }

  function isFiltered(key: string): boolean {
    const f = source.filters.get(key);
    return !!f && f.size > 0;
  }

  function toggleFilterValue(key: string, value: string): void {
    const current = new Set(source.filters.get(key) ?? []);
    if (current.has(value)) {
      current.delete(value);
    } else {
      current.add(value);
    }
    source.setFilter(key, current);
  }

  function getEffectiveFilterValues(col: ColumnSpec<T>): string[] {
    return col.filterValues ?? source.getFilterValues?.(col.key) ?? [];
  }

  function getFilterLabel(col: ColumnSpec<T>, value: string): string {
    return col.filterLabels?.[value] ?? value;
  }

  function currentEncodedFilter(key: string): string {
    return [...(source.filters.get(key) ?? [])][0] ?? '';
  }

  function applyEncodedFilter(key: string, encoded: string): void {
    source.setFilter(key, new Set([encoded]));
  }

  function clearFilter(key: string): void {
    source.setFilter(key, new Set());
  }
</script>

<TableHeader>
  <TableRow class={cn(stickyHeader && 'sticky top-0 z-10 bg-background')}>
    {#if detailPanel}
      <TableHead class="w-8 px-2"></TableHead>
    {/if}
    {#each visibleColumns as col (col.key)}
      {@const effectiveValues = getEffectiveFilterValues(col)}
      <TableHead
        class={cn(
          col.align === 'right' || col.type === 'numeric' || col.type === 'currency'
            ? 'text-right'
            : 'text-left',
          col.width && `w-[${col.width}]`,
          (col.sortable !== false || col.filterable) && 'select-none',
        )}
        aria-sort={ariaSort(col)}
      >
        <div
          class={cn(
            'flex items-center gap-1',
            (col.align === 'right' || col.type === 'numeric' || col.type === 'currency') &&
              'justify-end',
          )}
        >
          {#if col.sortable !== false}
            <button
              class="flex items-center gap-1 hover:text-foreground transition-colors text-muted-foreground"
              onclick={() => source.setSort(col.key, nextSortDir(col))}
            >
              <span class={source.sortKey === col.key ? 'text-foreground font-medium' : ''}>
                {col.header}
              </span>
              {#if source.sortKey === col.key}
                {#if source.sortDirection === 'asc'}
                  <ArrowUp class="h-3.5 w-3.5 text-foreground" />
                {:else}
                  <ArrowDown class="h-3.5 w-3.5 text-foreground" />
                {/if}
              {:else}
                <ArrowUpDown class="h-3.5 w-3.5 opacity-40" />
              {/if}
            </button>
          {:else}
            <span>{col.header}</span>
          {/if}

          {#if col.filterable && col.type === 'date'}
            <DateRangeFilterPopover
              columnLabel={col.header}
              isFiltered={isFiltered(col.key)}
              currentFilter={currentEncodedFilter(col.key)}
              onApply={(encoded) => applyEncodedFilter(col.key, encoded)}
              onClear={() => clearFilter(col.key)}
            />
          {:else if col.filterable && (col.type === 'numeric' || col.type === 'currency')}
            <NumericFilterPopover
              columnLabel={col.header}
              isFiltered={isFiltered(col.key)}
              unit={col.unit}
              currentFilter={currentEncodedFilter(col.key)}
              onApply={(encoded) => applyEncodedFilter(col.key, encoded)}
              onClear={() => clearFilter(col.key)}
            />
          {:else if col.filterable && effectiveValues.length > 0}
            <ListFilterPopover
              columnLabel={col.header}
              isFiltered={isFiltered(col.key)}
              values={effectiveValues}
              selectedValues={source.filters.get(col.key) ?? new Set()}
              getLabel={(v) => getFilterLabel(col, v)}
              onToggle={(v) => toggleFilterValue(col.key, v)}
              onClear={() => clearFilter(col.key)}
            />
          {/if}
        </div>
      </TableHead>
    {/each}
    {#if actions}
      <TableHead class="text-right w-[80px]"></TableHead>
    {/if}
  </TableRow>
</TableHeader>

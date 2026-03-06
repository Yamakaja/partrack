<script lang="ts" generics="T">
  import { ArrowUp, ArrowDown, ArrowUpDown, Filter, CalendarDays } from 'lucide-svelte';
  import { DropdownMenu as DropdownMenuPrimitive, Popover } from 'bits-ui';
  import { TableHeader, TableRow, TableHead } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
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

  function isValueSelected(key: string, value: string): boolean {
    return source.filters.get(key)?.has(value) ?? false;
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

  // Date range popover state — one per column key
  let datePopoverOpen = $state<Record<string, boolean>>({});
  let dateFrom = $state<Record<string, string>>({});
  let dateTo = $state<Record<string, string>>({});

  function openDatePopover(key: string): void {
    // Initialize date inputs from current filter value
    const encoded = [...(source.filters.get(key) ?? [])][0] ?? '|';
    const [from, to] = encoded.split('|');
    dateFrom[key] = from ?? '';
    dateTo[key] = to ?? '';
  }

  function applyDateFilter(key: string): void {
    const from = dateFrom[key] ?? '';
    const to = dateTo[key] ?? '';
    if (!from && !to) {
      source.setFilter(key, new Set());
    } else {
      source.setFilter(key, new Set([`${from}|${to}`]));
    }
    datePopoverOpen[key] = false;
  }

  function clearDateFilter(key: string): void {
    source.setFilter(key, new Set());
    dateFrom[key] = '';
    dateTo[key] = '';
    datePopoverOpen[key] = false;
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
            <!-- Date range popover -->
            <Popover.Root
              open={datePopoverOpen[col.key] ?? false}
              onOpenChange={(v) => { datePopoverOpen[col.key] = v; if (v) openDatePopover(col.key); }}
            >
              <Popover.Trigger>
                <button
                  class={cn(
                    'rounded p-0.5 hover:bg-accent transition-colors',
                    isFiltered(col.key) ? 'text-primary' : 'text-muted-foreground opacity-60 hover:opacity-100',
                  )}
                  aria-label="Filter {col.header} by date range"
                >
                  <CalendarDays class="h-3 w-3" />
                </button>
              </Popover.Trigger>
              <Popover.Content
                align="start"
                class="z-50 w-64 rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
              >
                <div class="flex flex-col gap-3">
                  <p class="text-xs font-medium text-muted-foreground">Filter by date range</p>
                  <div class="flex flex-col gap-2">
                    <label class="flex flex-col gap-1">
                      <span class="text-xs text-muted-foreground">From</span>
                      <input
                        type="date"
                        bind:value={dateFrom[col.key]}
                        class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                      />
                    </label>
                    <label class="flex flex-col gap-1">
                      <span class="text-xs text-muted-foreground">To</span>
                      <input
                        type="date"
                        bind:value={dateTo[col.key]}
                        class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                      />
                    </label>
                  </div>
                  <div class="flex gap-2">
                    <Button
                      size="sm"
                      class="flex-1 h-7 text-xs"
                      onclick={() => applyDateFilter(col.key)}
                    >
                      Apply
                    </Button>
                    {#if isFiltered(col.key)}
                      <Button
                        variant="outline"
                        size="sm"
                        class="h-7 text-xs"
                        onclick={() => clearDateFilter(col.key)}
                      >
                        Clear
                      </Button>
                    {/if}
                  </div>
                </div>
              </Popover.Content>
            </Popover.Root>

          {:else if col.filterable && effectiveValues.length > 0}
            <!-- Enum/text checkbox dropdown -->
            <DropdownMenuPrimitive.Root>
              <DropdownMenuPrimitive.Trigger>
                <button
                  class={cn(
                    'rounded p-0.5 hover:bg-accent transition-colors',
                    isFiltered(col.key) ? 'text-primary' : 'text-muted-foreground opacity-60 hover:opacity-100',
                  )}
                  aria-label="Filter {col.header}"
                >
                  <Filter class="h-3 w-3" />
                </button>
              </DropdownMenuPrimitive.Trigger>
              <DropdownMenuPrimitive.Content
                align="start"
                class="z-50 min-w-40 overflow-hidden rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
              >
                {#each effectiveValues as value (value)}
                  <DropdownMenuPrimitive.Item
                    class="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm cursor-pointer hover:bg-accent hover:text-accent-foreground outline-none select-none"
                    onSelect={() => toggleFilterValue(col.key, value)}
                  >
                    <span
                      class={cn(
                        'h-3.5 w-3.5 rounded-sm border border-primary flex-shrink-0',
                        isValueSelected(col.key, value) && 'bg-primary',
                      )}
                    ></span>
                    {getFilterLabel(col, value)}
                  </DropdownMenuPrimitive.Item>
                {/each}
                {#if isFiltered(col.key)}
                  <DropdownMenuPrimitive.Separator class="my-1 h-px bg-border" />
                  <DropdownMenuPrimitive.Item
                    class="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm cursor-pointer hover:bg-accent hover:text-accent-foreground outline-none select-none text-muted-foreground"
                    onSelect={() => source.setFilter(col.key, new Set())}
                  >
                    Clear filter
                  </DropdownMenuPrimitive.Item>
                {/if}
              </DropdownMenuPrimitive.Content>
            </DropdownMenuPrimitive.Root>
          {/if}
        </div>
      </TableHead>
    {/each}
    {#if actions}
      <TableHead class="text-right w-[80px]"></TableHead>
    {/if}
  </TableRow>
</TableHeader>

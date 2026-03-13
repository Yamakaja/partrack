<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { untrack } from 'svelte';
  import { Popover } from 'bits-ui';
  import { X, Hash, ListFilter } from 'lucide-svelte';
  import { listParts, getCategoryParameters, type ParamFilter } from '$lib/api/parts.js';
  import { listCategories, listPartCategories } from '$lib/api/categories.js';
  import type { PartSummary, CategoryParameter } from '$lib/types/parts.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { DataTable, RemoteDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { cn } from '$lib/utils/cn.js';
  import { formatSI, parseSI } from '$lib/utils/si.js';

  let {
    teamId,
    onRowClick,
    initialStockFilter = undefined,
    syncUrl = true,
  }: {
    teamId: string;
    onRowClick: (part: PartSummary) => void;
    initialStockFilter?: string;
    syncUrl?: boolean;
  } = $props();

  const hashParams = new URLSearchParams(untrack(() => syncUrl) ? window.location.hash.slice(1) : '');

  let searchValue = $state(hashParams.get('q') ?? '');
  let categoryFilter = $state<number | null>(
    hashParams.get('category_id') ? Number(hashParams.get('category_id')) : null,
  );
  // Per-param popover open state and numeric input state, keyed by param slug
  let filterOpenState = $state<Record<string, boolean>>({});
  let numericInputs = $state<Record<string, { min: string; max: string }>>({});

  const categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: listCategories,
    staleTime: Infinity,
  }));

  const partCategoriesQuery = createQuery(() => ({
    queryKey: ['teams', teamId, 'part-categories'],
    queryFn: () => listPartCategories(teamId),
    staleTime: 5 * 60 * 1000,
  }));

  const categoryParamsQuery = createQuery(() => ({
    queryKey: ['category-params', categoryFilter, teamId],
    queryFn: () =>
      categoryFilter != null
        ? getCategoryParameters(categoryFilter, teamId)
        : Promise.resolve<CategoryParameter[]>([]),
    enabled: categoryFilter != null,
    staleTime: 5 * 60 * 1000,
  }));

  const baseColumns: ColumnSpec<PartSummary>[] = [
    { key: 'mpn',              header: 'MPN',          type: 'text',    sortable: false },
    { key: 'manufacturer',     header: 'Manufacturer', type: 'text',    sortable: false },
    { key: 'description',      header: 'Description',  type: 'text',    sortable: false },
    { key: 'category_name',    header: 'Category',     type: 'text',    sortable: false },
    { key: 'lifecycle_status', header: 'Status',       type: 'enum',    sortable: false,
      filterable: true,
      filterValues: ['active', 'nrnd', 'obsolete', 'deprecated', 'no_longer_available'],
      filterLabels: {
        active: 'Active', nrnd: 'NRND', obsolete: 'Obsolete',
        deprecated: 'Deprecated', no_longer_available: 'NLA',
      },
    },
    { key: 'quantity_on_hand', header: 'Qty',          type: 'numeric', sortable: false, align: 'right' },
    { key: 'stock_status',     header: 'Stock',        type: 'enum',    sortable: false,
      filterable: true,
      filterValues: ['in_stock', 'low_stock', 'out_of_stock', 'not_tracked'],
      filterLabels: {
        in_stock: 'In stock', low_stock: 'Low stock',
        out_of_stock: 'Out of stock', not_tracked: 'Not tracked',
      },
    },
  ];

  const source = new RemoteDataSource<PartSummary>(
    baseColumns,
    async (params) => {
      const stockStatus = [...(params.filters.get('stock_status') ?? [])];
      const lifecycleStatus = [...(params.filters.get('lifecycle_status') ?? [])];
      const categoryIdStr = [...(params.filters.get('category_id') ?? [])][0] ?? undefined;

      // Convert param_* filter entries back to ParamFilter format for the API
      const catParams = categoryParamsQuery.data ?? [];
      const pf: Record<number, ParamFilter> = {};
      for (const [key, values] of params.filters) {
        if (!key.startsWith('param_')) continue;
        const slug = key.slice(6);
        const paramDef = catParams.find((cp) => cp.slug === slug);
        if (!paramDef) continue;
        if (paramDef.value_type === 'numeric') {
          const encoded = [...values][0] ?? '';
          let min: number | undefined, max: number | undefined;
          if (encoded.includes('|')) {
            const [lo, hi] = encoded.split('|');
            min = Number(lo); max = Number(hi);
          } else if (encoded.startsWith('>=')) { min = Number(encoded.slice(2)); }
          else if (encoded.startsWith('<='))   { max = Number(encoded.slice(2)); }
          else if (encoded.startsWith('='))    { min = max = Number(encoded.slice(1)); }
          if (min !== undefined || max !== undefined)
            pf[paramDef.id] = { type: 'numeric', min, max };
        } else {
          const vals = [...values];
          if (vals.length) pf[paramDef.id] = { type: 'enum', values: vals };
        }
      }

      const result = await listParts({
        team_id: teamId,
        q: (params.extra.q as string) || undefined,
        stock_status: stockStatus.length ? stockStatus : undefined,
        lifecycle_status: lifecycleStatus.length ? lifecycleStatus : undefined,
        category_id: categoryIdStr ? Number(categoryIdStr) : undefined,
        param_filters: Object.keys(pf).length ? pf : undefined,
        limit: params.pageSize,
        offset: (params.page - 1) * params.pageSize,
      });

      // Flatten parameters into param_${slug} keys for column rendering
      const items = result.items.map((p) => {
        if (!p.parameters) return p;
        const flat: Record<string, unknown> = {};
        for (const [slug, val] of Object.entries(p.parameters)) {
          if (val !== null && typeof val === 'object' && 'min' in val) {
            flat[`param_${slug}`] = (val as { min: number }).min;
            const maxVal = (val as { min: number; max?: number }).max;
            if (maxVal != null) flat[`param_${slug}__max`] = maxVal;
          } else {
            flat[`param_${slug}`] = val;
          }
        }
        return { ...p, ...flat } as PartSummary;
      });
      return { items, total: result.total };
    },
    () => ({ q: searchValue }),
    {
      pageSize: 50,
      onColumnHidden: (key, hidden) => {
        if (hidden && key.startsWith('param_')) source.setFilter(key, new Set());
      },
    },
  );

  // svelte-ignore state_referenced_locally
  {
    const ss = hashParams.get('stock_status');
    const ls = hashParams.get('lifecycle_status');
    const ci = hashParams.get('category_id');
    const pg = hashParams.get('page');
    if (ss) {
      source.setFilter('stock_status', new Set(ss.split(',')));
    } else if (initialStockFilter) {
      source.setFilter('stock_status', new Set([initialStockFilter]));
    }
    if (ls) source.setFilter('lifecycle_status', new Set(ls.split(',')));
    if (ci) source.setFilter('category_id', new Set([ci]));
    // Restore param filters from URL hash
    for (const [key, val] of hashParams.entries()) {
      if (key.startsWith('param_') && val) {
        source.setFilter(key, new Set([val]));
      }
    }
    if (pg) source.page = Number(pg);
  }

  // Rebuild columns whenever category parameters change
  $effect(() => {
    const params = categoryParamsQuery.data ?? [];
    const paramCols: ColumnSpec<PartSummary>[] = params.map((p) => {
      const sym = p.unit_symbol;
      const fmt = (n: number) =>
        sym ? formatSI(n, sym) : String(parseFloat(n.toPrecision(4)));
      return {
        key: `param_${p.slug}`,
        header: p.display_name + (sym ? ` (${sym})` : ''),
        type: 'text' as const,
        sortable: false,
        group: 'Parameters',
        cellText: (value, row) => {
          if (value == null || value === '') return '—';
          const maxVal = (row as unknown as Record<string, unknown>)[`param_${p.slug}__max`];
          if (p.value_type === 'numeric') {
            const n = Number(value);
            if (isNaN(n)) return String(value);
            const fmtMin = fmt(n);
            if (maxVal != null) {
              const m = Number(maxVal);
              const fmtMax = fmt(m);
              if (fmtMin !== fmtMax) return `${fmtMin}–${fmtMax}`;
            }
            return fmtMin;
          }
          return String(value);
        },
        filterFormatter: (values) => {
          const encoded = [...values][0] ?? '';
          const fmtV = (v: string) => {
            const n = Number(v);
            return isNaN(n) ? v : fmt(n);
          };
          if (encoded.includes('|')) {
            const [lo, hi] = encoded.split('|');
            return `${p.display_name}: ${fmtV(lo)}–${fmtV(hi)}`;
          }
          if (encoded.startsWith('>=')) return `${p.display_name}: ≥${fmtV(encoded.slice(2))}`;
          if (encoded.startsWith('<=')) return `${p.display_name}: ≤${fmtV(encoded.slice(2))}`;
          if (encoded.startsWith('='))  return `${p.display_name}: ${fmtV(encoded.slice(1))}`;
          // enum: multiple values in set
          const vals = [...values];
          if (vals.length > 2) return `${p.display_name}: ${vals.slice(0, 2).join(', ')} +${vals.length - 2}`;
          return `${p.display_name}: ${vals.join(', ')}`;
        },
      };
    });
    const insertAt = baseColumns.findIndex((c) => c.key === 'lifecycle_status');
    const newCols = [...baseColumns];
    newCols.splice(insertAt, 0, ...paramCols);
    source.setColumns(newCols);
  });

  // When category is cleared, also clear all param filters from source.filters
  $effect(() => {
    if (categoryFilter == null) {
      for (const key of [...source.filters.keys()]) {
        if (key.startsWith('param_')) source.setFilter(key, new Set());
      }
    }
  });

  $effect(() => {
    const params = new URLSearchParams();
    if (searchValue) params.set('q', searchValue);
    const ss = [...(source.filters.get('stock_status') ?? [])];
    if (ss.length) params.set('stock_status', ss.join(','));
    const ls = [...(source.filters.get('lifecycle_status') ?? [])];
    if (ls.length) params.set('lifecycle_status', ls.join(','));
    if (categoryFilter != null) params.set('category_id', String(categoryFilter));
    // Persist active param filters individually
    for (const [key, values] of source.filters) {
      if (key.startsWith('param_') && values.size > 0) {
        params.set(key, [...values][0]);
      }
    }
    if (source.page > 1) params.set('page', String(source.page));
    if (!syncUrl) return;
    const hash = params.toString();
    history.replaceState(null, '', window.location.pathname + (hash ? '#' + hash : ''));
  });

  export function refresh() {
    source.refresh();
  }

  export function setFilter(key: string, values: Set<string>) {
    source.setFilter(key, values);
  }

  export function setCategoryFilter(id: number | null) {
    categoryFilter = id;
    source.setFilter('category_id', id != null ? new Set([String(id)]) : new Set());
  }
</script>

{#snippet renderMpn(value: unknown, _row: PartSummary)}
  <span class="font-mono font-medium text-xs">{value ?? '—'}</span>
{/snippet}

{#snippet renderLifecycle(value: unknown, _row: PartSummary)}
  <LifecycleBadge status={value as PartSummary['lifecycle_status']} />
{/snippet}

{#snippet renderQty(_value: unknown, row: PartSummary)}
  <span class="tabular-nums">
    {row.inventory_entry_id != null ? (row.quantity_on_hand ?? 0) : '—'}
  </span>
{/snippet}

{#snippet renderStock(_value: unknown, row: PartSummary)}
  {#if row.inventory_entry_id != null}
    <StockBadge
      quantity_on_hand={row.quantity_on_hand ?? 0}
      minimum_stock_quantity={row.minimum_stock_quantity}
    />
  {:else}
    <span class="text-xs text-muted-foreground">Not tracked</span>
  {/if}
{/snippet}

{#snippet renderDescription(_value: unknown, row: PartSummary)}
  {row.short_description ?? row.description ?? ''}
{/snippet}

{#snippet columnParamFilter(col: ColumnSpec<PartSummary>)}
  {#if col.key.startsWith('param_')}
    {@const slug = col.key.slice(6)}
    {@const param = categoryParamsQuery.data?.find((p) => p.slug === slug)}
    {#if param?.value_type === 'numeric'}
      {@const isFiltered = (source.filters.get(col.key)?.size ?? 0) > 0}
      <Popover.Root
        open={filterOpenState[slug] ?? false}
        onOpenChange={(v) => {
          if (v) {
            const encoded = [...(source.filters.get(col.key) ?? [])][0] ?? '';
            let minStr = '', maxStr = '';
            if (encoded.includes('|')) {
              const [lo, hi] = encoded.split('|');
              minStr = param.unit_symbol ? formatSI(Number(lo), param.unit_symbol) : lo;
              maxStr = param.unit_symbol ? formatSI(Number(hi), param.unit_symbol) : hi;
            } else if (encoded.startsWith('>=')) {
              const v2 = encoded.slice(2);
              minStr = param.unit_symbol ? formatSI(Number(v2), param.unit_symbol) : v2;
            } else if (encoded.startsWith('<=')) {
              const v2 = encoded.slice(2);
              maxStr = param.unit_symbol ? formatSI(Number(v2), param.unit_symbol) : v2;
            }
            numericInputs = { ...numericInputs, [slug]: { min: minStr, max: maxStr } };
          }
          filterOpenState = { ...filterOpenState, [slug]: v };
        }}
      >
        <Popover.Trigger
          class={cn(
            'inline-flex items-center gap-1 rounded-md border px-1.5 h-6 text-xs transition-colors',
            isFiltered
              ? 'border-primary bg-primary/10 text-primary'
              : 'border-border text-muted-foreground hover:text-foreground hover:border-foreground/30',
          )}
          onclick={(e) => e.stopPropagation()}
        >
          <Hash class="h-3 w-3 flex-shrink-0" />
          {#if isFiltered}<X class="h-2.5 w-2.5" onclick={(e) => { e.stopPropagation(); source.setFilter(col.key, new Set()); }} />{/if}
        </Popover.Trigger>
        <Popover.Content
          align="start"
          class="z-50 w-52 rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
        >
          <p class="text-xs font-medium mb-2">
            {param.display_name}{param.unit_symbol ? ` (${param.unit_symbol})` : ''}
          </p>
          <div class="flex flex-col gap-2 mb-3">
            <label class="flex flex-col gap-1">
              <span class="text-xs text-muted-foreground">
                Min{param.min_numeric != null && param.unit_symbol
                  ? ` (e.g. ${formatSI(param.min_numeric, param.unit_symbol)})`
                  : param.min_numeric != null ? ` (e.g. ${param.min_numeric})` : ''}
              </span>
              <input
                type="text"
                value={numericInputs[slug]?.min ?? ''}
                oninput={(e) => {
                  numericInputs = { ...numericInputs, [slug]: { ...numericInputs[slug], min: (e.target as HTMLInputElement).value } };
                }}
                class="h-7 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
              />
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-xs text-muted-foreground">
                Max{param.max_numeric != null && param.unit_symbol
                  ? ` (e.g. ${formatSI(param.max_numeric, param.unit_symbol)})`
                  : param.max_numeric != null ? ` (e.g. ${param.max_numeric})` : ''}
              </span>
              <input
                type="text"
                value={numericInputs[slug]?.max ?? ''}
                oninput={(e) => {
                  numericInputs = { ...numericInputs, [slug]: { ...numericInputs[slug], max: (e.target as HTMLInputElement).value } };
                }}
                class="h-7 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
              />
            </label>
          </div>
          <div class="flex gap-2">
            <Button
              size="sm"
              class="flex-1 h-7 text-xs"
              onclick={() => {
                const inputs = numericInputs[slug] ?? { min: '', max: '' };
                const minVal = parseSI(inputs.min, param.unit_symbol);
                const maxVal = parseSI(inputs.max, param.unit_symbol);
                if (minVal == null && maxVal == null) {
                  source.setFilter(col.key, new Set());
                } else if (minVal != null && maxVal != null && minVal === maxVal) {
                  source.setFilter(col.key, new Set([`=${minVal}`]));
                } else if (minVal != null && maxVal != null) {
                  source.setFilter(col.key, new Set([`${minVal}|${maxVal}`]));
                } else if (minVal != null) {
                  source.setFilter(col.key, new Set([`>=${minVal}`]));
                } else if (maxVal != null) {
                  source.setFilter(col.key, new Set([`<=${maxVal}`]));
                }
                filterOpenState = { ...filterOpenState, [slug]: false };
              }}
            >Apply</Button>
            {#if isFiltered}
              <Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => { source.setFilter(col.key, new Set()); filterOpenState = { ...filterOpenState, [slug]: false }; }}>Clear</Button>
            {/if}
          </div>
        </Popover.Content>
      </Popover.Root>

    {:else if param?.value_type === 'enum' || param?.value_type === 'text'}
      {@const selectedValues = source.filters.get(col.key) ?? new Set<string>()}
      {@const isFiltered = selectedValues.size > 0}
      <Popover.Root
        open={filterOpenState[slug] ?? false}
        onOpenChange={(v) => filterOpenState = { ...filterOpenState, [slug]: v }}
      >
        <Popover.Trigger
          class={cn(
            'inline-flex items-center gap-1 rounded-md border px-1.5 h-6 text-xs transition-colors',
            isFiltered
              ? 'border-primary bg-primary/10 text-primary'
              : 'border-border text-muted-foreground hover:text-foreground hover:border-foreground/30',
          )}
          onclick={(e) => e.stopPropagation()}
        >
          <ListFilter class="h-3 w-3 flex-shrink-0" />
          {#if isFiltered}<span class="text-xs">{selectedValues.size}</span><X class="h-2.5 w-2.5" onclick={(e) => { e.stopPropagation(); source.setFilter(col.key, new Set()); }} />{/if}
        </Popover.Trigger>
        <Popover.Content
          align="start"
          class="z-50 w-52 rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
        >
          <div class="max-h-52 overflow-y-auto py-1 select-none">
            {#if (param.enum_values?.length ?? 0) === 0}
              <p class="px-2 py-1.5 text-xs text-muted-foreground">No values</p>
            {:else}
              {#each param.enum_values as value (value)}
                <button
                  class="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground text-left"
                  onclick={() => {
                    const next = new Set(selectedValues);
                    if (next.has(value)) next.delete(value); else next.add(value);
                    source.setFilter(col.key, next);
                  }}
                >
                  <span class={cn('h-3.5 w-3.5 flex-shrink-0 rounded-sm border border-primary transition-colors', selectedValues.has(value) && 'bg-primary')}></span>
                  {value}
                </button>
              {/each}
            {/if}
          </div>
          {#if isFiltered}
            <div class="border-t border-border px-2 py-1.5 flex items-center justify-between">
              <span class="text-xs text-muted-foreground">{selectedValues.size} selected</span>
              <button class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2" onclick={() => source.setFilter(col.key, new Set())}>Clear</button>
            </div>
          {/if}
        </Popover.Content>
      </Popover.Root>
    {/if}
  {/if}
{/snippet}

<DataTable
  {source}
  getRowKey={(p) => p.id}
  onRowClick={onRowClick}
  renderers={{
    mpn: renderMpn,
    description: renderDescription,
    lifecycle_status: renderLifecycle,
    quantity_on_hand: renderQty,
    stock_status: renderStock,
  }}
  columnHeaderFilter={columnParamFilter}
  emptyMessage="No parts found"
  exportFilename="parts.csv"
  showColumnPicker={true}
>
  {#snippet toolbarLeft()}
    <SearchInput
      value={searchValue}
      placeholder="Search MPN, description, manufacturer…"
      onchange={(v) => { searchValue = v; }}
    />
    <div class="w-80 flex-shrink-0">
      <CategoryPicker
        value={categoryFilter}
        categories={categoriesQuery.data ?? []}
        availableIds={partCategoriesQuery.data}
        placeholder="All categories"
        onSelect={(id) => {
          categoryFilter = id;
          source.setFilter('category_id', id != null ? new Set([String(id)]) : new Set());
        }}
      />
    </div>
  {/snippet}
</DataTable>

<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { Popover } from 'bits-ui';
  import { X, Hash, Filter } from 'lucide-svelte';
  import { listParts, getCategoryParameters, type ParamFilter } from '$lib/api/parts.js';
  import { listCategories, listPartCategories } from '$lib/api/categories.js';
  import type { PartSummary, CategoryParameter } from '$lib/types/parts.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { DataTable, RemoteDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { cn } from '$lib/utils/cn.js';
  import { formatSI, parseSI } from '$lib/utils/si.js';

  let {
    teamId,
    onRowClick,
    initialStockFilter = undefined,
  }: {
    teamId: string;
    onRowClick: (part: PartSummary) => void;
    initialStockFilter?: string;
  } = $props();

  const hashParams = new URLSearchParams(window.location.hash.slice(1));

  let searchValue = $state(hashParams.get('q') ?? '');
  let categoryFilter = $state<number | null>(
    hashParams.get('category_id') ? Number(hashParams.get('category_id')) : null,
  );
  let paramFilters = $state<Record<number, ParamFilter>>((() => {
    const pf = hashParams.get('pf');
    if (!pf) return {};
    try { return JSON.parse(pf); } catch { return {}; }
  })());
  let filterOpenState = $state<Record<number, boolean>>({});
  // Local min/max text inputs for each open numeric filter popover
  let numericInputs = $state<Record<number, { min: string; max: string }>>({});

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

  const columns: ColumnSpec<PartSummary>[] = [
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
    columns,
    async (params) => {
      const stockStatus = [...(params.filters.get('stock_status') ?? [])];
      const lifecycleStatus = [...(params.filters.get('lifecycle_status') ?? [])];
      const categoryIdStr = [...(params.filters.get('category_id') ?? [])][0] ?? undefined;
      const pf = params.extra.paramFilters as Record<number, ParamFilter> | undefined;
      return listParts({
        team_id: teamId,
        q: (params.extra.q as string) || undefined,
        stock_status: stockStatus.length ? stockStatus : undefined,
        lifecycle_status: lifecycleStatus.length ? lifecycleStatus : undefined,
        category_id: categoryIdStr ? Number(categoryIdStr) : undefined,
        param_filters: pf && Object.keys(pf).length ? pf : undefined,
        limit: params.pageSize,
        offset: (params.page - 1) * params.pageSize,
      });
    },
    () => ({ q: searchValue, paramFilters }),
    { pageSize: 50 },
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
    if (pg) source.page = Number(pg);
  }

  $effect(() => {
    const params = new URLSearchParams();
    if (searchValue) params.set('q', searchValue);
    const ss = [...(source.filters.get('stock_status') ?? [])];
    if (ss.length) params.set('stock_status', ss.join(','));
    const ls = [...(source.filters.get('lifecycle_status') ?? [])];
    if (ls.length) params.set('lifecycle_status', ls.join(','));
    if (categoryFilter != null) params.set('category_id', String(categoryFilter));
    if (Object.keys(paramFilters).length) params.set('pf', JSON.stringify(paramFilters));
    if (source.page > 1) params.set('page', String(source.page));
    const hash = params.toString();
    history.replaceState(null, '', window.location.pathname + (hash ? '#' + hash : ''));
  });

  function clearParamFilters() {
    paramFilters = {};
  }

  function applyNumericFilter(paramId: number, min?: number, max?: number) {
    if (min === undefined && max === undefined) { clearNumericFilter(paramId); return; }
    paramFilters = { ...paramFilters, [paramId]: { type: 'numeric', min, max } };
    filterOpenState = { ...filterOpenState, [paramId]: false };
  }

  function clearNumericFilter(paramId: number) {
    const { [paramId]: _, ...rest } = paramFilters;
    paramFilters = rest as typeof paramFilters;
    filterOpenState = { ...filterOpenState, [paramId]: false };
  }

  function toggleEnumValue(paramId: number, value: string) {
    const current = new Set<string>(
      paramFilters[paramId]?.type === 'enum' ? (paramFilters[paramId].values ?? []) : [],
    );
    if (current.has(value)) current.delete(value); else current.add(value);
    if (current.size > 0) {
      paramFilters = { ...paramFilters, [paramId]: { type: 'enum', values: [...current] } };
    } else {
      const { [paramId]: _, ...rest } = paramFilters;
      paramFilters = rest as typeof paramFilters;
    }
  }

  function clearEnumFilter(paramId: number) {
    const { [paramId]: _, ...rest } = paramFilters;
    paramFilters = rest as typeof paramFilters;
    filterOpenState = { ...filterOpenState, [paramId]: false };
  }

  function numericFilterLabel(param: CategoryParameter): string {
    const pf = paramFilters[param.id];
    if (!pf || pf.type !== 'numeric') return param.display_name;
    const fmt = (v: number) => param.unit_symbol ? formatSI(v, param.unit_symbol) : String(parseFloat(v.toPrecision(4)));
    if (pf.min !== undefined && pf.max !== undefined && pf.min === pf.max)
      return `${param.display_name}: ${fmt(pf.min)}`;
    if (pf.min !== undefined && pf.max !== undefined)
      return `${param.display_name}: ${fmt(pf.min)}–${fmt(pf.max)}`;
    if (pf.min !== undefined) return `${param.display_name}: ≥${fmt(pf.min)}`;
    if (pf.max !== undefined) return `${param.display_name}: ≤${fmt(pf.max)}`;
    return param.display_name;
  }

  function enumFilterLabel(param: CategoryParameter): string {
    const pf = paramFilters[param.id];
    if (!pf || pf.type !== 'enum' || !pf.values?.length) return param.display_name;
    if (pf.values.length <= 2) return `${param.display_name}: ${pf.values.join(', ')}`;
    return `${param.display_name}: ${pf.values.slice(0, 2).join(', ')} +${pf.values.length - 2}`;
  }

  const activeParamFilterCount = $derived(Object.keys(paramFilters).length);

  export function refresh() {
    source.refresh();
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
  emptyMessage="No parts found"
  exportFilename="parts.csv"
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
          clearParamFilters();
        }}
      />
    </div>

    {#if categoryFilter != null && (categoryParamsQuery.data?.length ?? 0) > 0}
      {#each categoryParamsQuery.data ?? [] as param (param.id)}
        {#if param.value_type === 'numeric'}
          {@const isFiltered = !!paramFilters[param.id]}
          <Popover.Root
            open={filterOpenState[param.id] ?? false}
            onOpenChange={(v) => {
              if (v) {
                const pf = paramFilters[param.id];
                const toStr = (n: number | undefined) =>
                  n !== undefined
                    ? (param.unit_symbol ? formatSI(n, param.unit_symbol) : String(parseFloat(n.toPrecision(4))))
                    : '';
                numericInputs = {
                  ...numericInputs,
                  [param.id]: {
                    min: pf?.type === 'numeric' ? toStr(pf.min) : '',
                    max: pf?.type === 'numeric' ? toStr(pf.max) : '',
                  },
                };
              }
              filterOpenState = { ...filterOpenState, [param.id]: v };
            }}
          >
            <Popover.Trigger>
              <button
                class={cn(
                  'inline-flex items-center gap-1 rounded-md border px-2 h-7 text-xs transition-colors',
                  isFiltered
                    ? 'border-primary bg-primary/10 text-primary'
                    : 'border-border bg-background text-muted-foreground hover:text-foreground hover:border-foreground/30',
                )}
              >
                <Hash class="h-3 w-3 flex-shrink-0" />
                <span>{isFiltered ? numericFilterLabel(param) : param.display_name}</span>
                {#if isFiltered}
                  <span
                    class="ml-0.5 rounded-full hover:bg-primary/20 p-0.5 -mr-0.5 cursor-pointer"
                    role="button"
                    tabindex="0"
                    aria-label="Clear {param.display_name} filter"
                    onclick={(e) => { e.stopPropagation(); clearNumericFilter(param.id); }}
                    onkeydown={(e) => e.key === 'Enter' && (e.stopPropagation(), clearNumericFilter(param.id))}
                  >
                    <X class="h-2.5 w-2.5" />
                  </span>
                {/if}
              </button>
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
                    value={numericInputs[param.id]?.min ?? ''}
                    oninput={(e) => {
                      numericInputs = {
                        ...numericInputs,
                        [param.id]: { ...numericInputs[param.id], min: (e.target as HTMLInputElement).value },
                      };
                    }}
                    placeholder={param.min_numeric != null && param.unit_symbol
                      ? formatSI(param.min_numeric, param.unit_symbol)
                      : param.min_numeric != null ? String(param.min_numeric) : ''}
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
                    value={numericInputs[param.id]?.max ?? ''}
                    oninput={(e) => {
                      numericInputs = {
                        ...numericInputs,
                        [param.id]: { ...numericInputs[param.id], max: (e.target as HTMLInputElement).value },
                      };
                    }}
                    placeholder={param.max_numeric != null && param.unit_symbol
                      ? formatSI(param.max_numeric, param.unit_symbol)
                      : param.max_numeric != null ? String(param.max_numeric) : ''}
                    class="h-7 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                  />
                </label>
              </div>
              <div class="flex gap-2">
                <Button
                  size="sm"
                  class="flex-1 h-7 text-xs"
                  onclick={() => {
                    const inputs = numericInputs[param.id] ?? { min: '', max: '' };
                    const minVal = parseSI(inputs.min, param.unit_symbol);
                    const maxVal = parseSI(inputs.max, param.unit_symbol);
                    applyNumericFilter(param.id, minVal ?? undefined, maxVal ?? undefined);
                  }}
                >Apply</Button>
                {#if isFiltered}
                  <Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => clearNumericFilter(param.id)}>Clear</Button>
                {/if}
              </div>
            </Popover.Content>
          </Popover.Root>

        {:else if param.value_type === 'enum' || param.value_type === 'text'}
          {@const isFiltered = !!paramFilters[param.id]}
          {@const selectedValues = new Set<string>(
            paramFilters[param.id]?.type === 'enum' ? (paramFilters[param.id].values ?? []) : [],
          )}
          <Popover.Root
            open={filterOpenState[param.id] ?? false}
            onOpenChange={(v) => filterOpenState = { ...filterOpenState, [param.id]: v }}
          >
            <Popover.Trigger>
              <button
                class={cn(
                  'inline-flex items-center gap-1 rounded-md border px-2 h-7 text-xs transition-colors',
                  isFiltered
                    ? 'border-primary bg-primary/10 text-primary'
                    : 'border-border bg-background text-muted-foreground hover:text-foreground hover:border-foreground/30',
                )}
              >
                <Filter class="h-3 w-3 flex-shrink-0" />
                <span>{isFiltered ? enumFilterLabel(param) : param.display_name}</span>
                {#if isFiltered}
                  <span
                    class="ml-0.5 rounded-full hover:bg-primary/20 p-0.5 -mr-0.5 cursor-pointer"
                    role="button"
                    tabindex="0"
                    aria-label="Clear {param.display_name} filter"
                    onclick={(e) => { e.stopPropagation(); clearEnumFilter(param.id); }}
                    onkeydown={(e) => e.key === 'Enter' && (e.stopPropagation(), clearEnumFilter(param.id))}
                  >
                    <X class="h-2.5 w-2.5" />
                  </span>
                {/if}
              </button>
            </Popover.Trigger>
            <Popover.Content
              align="start"
              class="z-50 w-52 rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
            >
              <div class="max-h-52 overflow-y-auto py-1 select-none">
                {#if param.enum_values.length === 0}
                  <p class="px-2 py-1.5 text-xs text-muted-foreground">No values</p>
                {:else}
                  {#each param.enum_values as value (value)}
                    <button
                      class="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground text-left"
                      onclick={() => toggleEnumValue(param.id, value)}
                    >
                      <span
                        class={cn(
                          'h-3.5 w-3.5 flex-shrink-0 rounded-sm border border-primary transition-colors',
                          selectedValues.has(value) && 'bg-primary',
                        )}
                      ></span>
                      {value}
                    </button>
                  {/each}
                {/if}
              </div>
              {#if isFiltered}
                <div class="border-t border-border px-2 py-1.5 flex items-center justify-between">
                  <span class="text-xs text-muted-foreground">{selectedValues.size} selected</span>
                  <button
                    class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
                    onclick={() => clearEnumFilter(param.id)}
                  >Clear</button>
                </div>
              {/if}
            </Popover.Content>
          </Popover.Root>
        {/if}
      {/each}

      {#if activeParamFilterCount > 1}
        <button
          class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2 whitespace-nowrap"
          onclick={clearParamFilters}
        >
          Clear params
        </button>
      {/if}
    {/if}
  {/snippet}
</DataTable>

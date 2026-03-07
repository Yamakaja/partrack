<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { listParts } from '$lib/api/parts.js';
  import { listCategories, listPartCategories } from '$lib/api/categories.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import { DataTable, RemoteDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';

  let {
    teamId,
    onRowClick,
    initialStockFilter = undefined,
  }: {
    teamId: string;
    onRowClick: (part: PartSummary) => void;
    initialStockFilter?: string;
  } = $props();

  let searchValue = $state('');
  let categoryFilter = $state<number | null>(null);

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
      return listParts({
        team_id: teamId,
        q: (params.extra.q as string) || undefined,
        stock_status: stockStatus.length ? stockStatus : undefined,
        lifecycle_status: lifecycleStatus.length ? lifecycleStatus : undefined,
        category_id: categoryIdStr ? Number(categoryIdStr) : undefined,
        limit: params.pageSize,
        offset: (params.page - 1) * params.pageSize,
      });
    },
    () => ({ q: searchValue }),
    { pageSize: 50 },
  );

  // svelte-ignore state_referenced_locally
    if (initialStockFilter) {
    source.setFilter('stock_status', new Set([initialStockFilter]));
  }

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

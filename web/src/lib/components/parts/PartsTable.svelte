<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { untrack } from 'svelte';
  import { Popover } from 'bits-ui';
  import { X, Hash, ListFilter, ListPlus, RefreshCw, Sparkles } from 'lucide-svelte';
  import { currentRoute } from '$lib/router/index.svelte.js';
  import { listParts, refreshSearchListings, refreshSearchParameters, getCategoryParameters, partsSearchQuery, type ParamFilter, type ListPartsParams } from '$lib/api/parts.js';
  import { listCollections, createCollection, addSearchResultsToCollection } from '$lib/api/collections.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { listPanelStore, setActiveList } from '$lib/stores/listPanel.svelte.js';
  import { trackListingRefresh, trackPartEnrich } from '$lib/api/batchJob.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { listCategories, listPartCategories } from '$lib/api/categories.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import type { PartSummary, CategoryParameter } from '$lib/types/parts.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import StorageLocationFilterPicker from '$lib/components/storage/StorageLocationFilterPicker.svelte';
  import type { StorageLocationFilter } from '$lib/components/storage/StorageLocationFilterPicker.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import PartCell from '$lib/components/parts/PartCell.svelte';
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

  const qc = useQueryClient();

  const hashParams = new URLSearchParams(untrack(() => syncUrl) ? window.location.hash.slice(1) : '');

  let searchValue = $state(hashParams.get('q') ?? '');
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

  const storageQuery = createQuery(() => ({
    queryKey: ['storage-locations', teamId],
    queryFn: () => listStorageLocations(teamId),
    staleTime: 5 * 60 * 1000,
  }));

  const baseColumns: ColumnSpec<PartSummary>[] = [
    { key: 'mpn',              header: 'Part',         type: 'text',    sortable: false },
    // Folded into the Part cell visually; kept as export-only columns so the
    // CSV still carries the discrete fields.
    { key: 'manufacturer',     header: 'Manufacturer', type: 'text',    sortable: false, exportOnly: true },
    { key: 'description',      header: 'Description',  type: 'text',    sortable: false, exportOnly: true },
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

  // Builds the ListParts filter set from the active filters + search query.
  // Shared by the table fetcher and the bulk stock-refresh button so the refresh
  // is scoped to exactly the parts the current search returns.
  function buildListPartsParams(filters: Map<string, Set<string>>, q: string): ListPartsParams {
    const stockStatus = [...(filters.get('stock_status') ?? [])];
    const lifecycleStatus = [...(filters.get('lifecycle_status') ?? [])];
    const categoryIdStr = [...(filters.get('category_id') ?? [])][0] ?? undefined;

    // Convert param_* filter entries back to ParamFilter format for the API
    const catParams = categoryParamsQuery.data ?? [];
    const pf: Record<number, ParamFilter> = {};
    for (const [key, values] of filters) {
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

    const storageLocationId = [...(filters.get('storage_location_id') ?? [])][0] ?? undefined;
    const storageCellStrs = [...(filters.get('storage_cells') ?? [])];

    return {
      team_id: teamId,
      q: q || undefined,
      stock_status: stockStatus.length ? stockStatus : undefined,
      lifecycle_status: lifecycleStatus.length ? lifecycleStatus : undefined,
      category_id: categoryIdStr ? Number(categoryIdStr) : undefined,
      param_filters: Object.keys(pf).length ? pf : undefined,
      storage_location_id: storageLocationId,
      storage_cells: storageCellStrs.length ? storageCellStrs.join('|') : undefined,
    };
  }

  // Both bulk actions are scoped to the current search and gated behind a
  // confirmation dialog (they make many live distributor API calls).
  let refreshListingsConfirmOpen = $state(false);
  let refreshParamsConfirmOpen = $state(false);
  let refreshingListings = $state(false);
  let refreshingParams = $state(false);

  async function doRefreshListings() {
    refreshingListings = true;
    try {
      const result = await refreshSearchListings(teamId, buildListPartsParams(source.filters, searchValue));
      if (!result || result.listing_count === 0) {
        pushToast('info', 'Nothing to refresh', 'No parts in the current results have a distributor integration.');
        return;
      }
      trackListingRefresh({
        teamId,
        jobId: result.job_id,
        total: result.listing_count,
        onDone: () => source.refresh(),
      });
    } catch (err: any) {
      pushToast('error', 'Refresh failed', err.message);
    } finally {
      refreshingListings = false;
    }
  }

  async function doRefreshParameters() {
    refreshingParams = true;
    try {
      const result = await refreshSearchParameters(teamId, buildListPartsParams(source.filters, searchValue));
      if (!result || result.part_count === 0) {
        pushToast('info', 'Nothing to refresh', 'No parts in the current results have a distributor integration.');
        return;
      }
      trackPartEnrich({
        teamId,
        jobId: result.job_id,
        total: result.part_count,
        onDone: () => source.refresh(),
      });
    } catch (err: any) {
      pushToast('error', 'Parameter refresh failed', err.message);
    } finally {
      refreshingParams = false;
    }
  }

  // "Add to list": push every part in the current search — all pages, resolved
  // server-side from the same filters — into the panel's active list. Lists are
  // a personal working set, so this is gated on any non-viewer role rather than
  // the purchaser role the distributor refreshes imply.
  const canUseLists = $derived(
    activeTeamStore.current?.teamId === teamId &&
      (activeTeamStore.current?.role ?? 'viewer') !== 'viewer',
  );
  const collectionsQuery = createQuery(() => ({
    queryKey: ['collections', teamId],
    queryFn: () => listCollections(teamId),
    enabled: canUseLists,
    staleTime: 30_000,
  }));
  const activeList = $derived(
    collectionsQuery.data?.collections.find((c) => c.id === listPanelStore.activeListId) ?? null,
  );

  let addToListConfirmOpen = $state(false);
  let addingToList = $state(false);

  async function doAddToList() {
    addingToList = true;
    try {
      let listId = listPanelStore.activeListId;
      let name = activeList?.name;
      if (!listId) {
        const created = await createCollection(teamId);
        setActiveList(created.id);
        listId = created.id;
        name = created.name;
      }
      const res = await addSearchResultsToCollection(
        teamId, listId, partsSearchQuery(buildListPartsParams(source.filters, searchValue)),
      );
      qc.invalidateQueries({ queryKey: ['collections', teamId] });
      qc.invalidateQueries({ queryKey: ['collection', teamId, listId] });
      if (res.added === 0) {
        pushToast('info', 'Nothing added', 'No parts matched the current search.');
      } else {
        pushToast('success', `Added ${res.added} part${res.added === 1 ? '' : 's'} to “${name ?? 'list'}”`);
      }
    } catch (err: any) {
      pushToast('error', 'Could not add to list', err.message);
    } finally {
      addingToList = false;
    }
  }

  const source = new RemoteDataSource<PartSummary>(
    baseColumns,
    async (params) => {
      const result = await listParts({
        ...buildListPartsParams(params.filters, (params.extra.q as string) || ''),
        limit: params.pageSize,
        offset: (params.page - 1) * params.pageSize,
      });

      // Flatten parameter display values into param_${slug} keys for column
      // rendering. Values are the original, unaltered source text (e.g.
      // "100 Ohm @ 1 MHz"); the normalized values used for filtering live
      // server-side, not on the row.
      const items = result.items.map((p) => {
        if (!p.parameters) return p;
        const flat: Record<string, unknown> = {};
        for (const [slug, val] of Object.entries(p.parameters)) {
          flat[`param_${slug}`] = val;
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

  // Derived from source.filters so clearing a chip updates the pickers automatically
  const categoryFilter = $derived.by(() => {
    const val = [...(source.filters.get('category_id') ?? [])][0];
    return val != null ? Number(val) : null;
  });
  const storageFilter = $derived.by((): StorageLocationFilter | null => {
    const sid = [...(source.filters.get('storage_location_id') ?? [])][0];
    if (!sid) return null;
    const sc = [...(source.filters.get('storage_cells') ?? [])];
    const cells = sc.length ? sc.map((s) => s.split(',').map(Number)) : null;
    return { locationId: sid, cells };
  });

  const categoryParamsQuery = createQuery(() => ({
    queryKey: ['category-params', categoryFilter, teamId],
    queryFn: () =>
      categoryFilter != null
        ? getCategoryParameters(categoryFilter, teamId)
        : Promise.resolve<CategoryParameter[]>([]),
    enabled: categoryFilter != null,
    staleTime: 5 * 60 * 1000,
  }));

  // svelte-ignore state_referenced_locally
  {
    const ss = hashParams.get('stock_status');
    const ls = hashParams.get('lifecycle_status');
    const ci = hashParams.get('category_id');
    const pg = hashParams.get('page');
    const sid = hashParams.get('storage_location_id');
    const sc = hashParams.get('storage_cells');
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
    if (sid) {
      source.setFilter('storage_location_id', new Set([sid]));
      if (sc) source.setFilter('storage_cells', new Set(sc.split('|')));
    }
    if (pg) source.page = Number(pg);
  }

  // Re-apply hash params when navigate() changes the hash while component stays mounted
  // (e.g. command palette category navigation, or clicking nav "Parts" link to clear filters)
  // svelte-ignore state_referenced_locally
  const mountRouteId = untrack(() => currentRoute().id);

  $effect(() => {
    const route = currentRoute();
    if (route.id <= mountRouteId || !syncUrl) return;

    // untrack the body so reads of source.filters don't make this effect depend on
    // filter state — otherwise each setFilter call would re-trigger the effect.
    untrack(() => {
      const params = new URLSearchParams(route.hash.slice(1));

      source.setFilter('stock_status', new Set(params.get('stock_status')?.split(',') ?? []));
      source.setFilter('lifecycle_status', new Set(params.get('lifecycle_status')?.split(',') ?? []));
      source.setFilter('category_id', new Set(params.has('category_id') ? [params.get('category_id')!] : []));
      source.setFilter('storage_location_id', new Set(params.has('storage_location_id') ? [params.get('storage_location_id')!] : []));
      source.setFilter('storage_cells', new Set(params.get('storage_cells')?.split('|') ?? []));
      for (const key of [...source.filters.keys()]) {
        if (key.startsWith('param_')) source.setFilter(key, new Set());
      }
      for (const [key, val] of params.entries()) {
        if (key.startsWith('param_') && val) source.setFilter(key, new Set([val]));
      }
      searchValue = params.get('q') ?? '';
      source.page = params.has('page') ? Number(params.get('page')) : 1;
    });
  });

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
        // Show the original, unaltered value text verbatim. The normalized numeric
        // form (via fmt) is reserved for the filter chip label below.
        cellText: (value) => {
          if (value == null || value === '') return '—';
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
    const sid = [...(source.filters.get('storage_location_id') ?? [])][0];
    if (sid) params.set('storage_location_id', sid);
    const sc = [...(source.filters.get('storage_cells') ?? [])];
    if (sc.length) params.set('storage_cells', sc.join('|'));
    // Persist active param filters individually
    for (const [key, values] of source.filters) {
      if (key.startsWith('param_') && values.size > 0) {
        params.set(key, [...values][0]);
      }
    }
    if (source.page > 1) params.set('page', String(source.page));
    if (!syncUrl) return;
    const hash = params.toString();
    const url = window.location.pathname + (hash ? '#' + hash : '');
    // Only push a history entry when the synced URL actually changes. On mount — and
    // whenever filters are restored from the URL (popstate, command-palette nav) — the
    // computed URL equals the current one; pushing it again inserts a duplicate entry
    // that silently swallows the next Back press.
    if (url !== window.location.pathname + window.location.hash) {
      history.pushState(null, '', url);
    }
  });

  function applyParamFilter(slug: string, filterKey: string, unitSymbol?: string) {
    const inputs = numericInputs[slug] ?? { min: '', max: '' };
    const minVal = parseSI(inputs.min, unitSymbol);
    const maxVal = parseSI(inputs.max, unitSymbol);
    if (minVal == null && maxVal == null) {
      source.setFilter(filterKey, new Set());
    } else if (minVal != null && maxVal != null && minVal === maxVal) {
      source.setFilter(filterKey, new Set([`=${minVal}`]));
    } else if (minVal != null && maxVal != null) {
      source.setFilter(filterKey, new Set([`${minVal}|${maxVal}`]));
    } else if (minVal != null) {
      source.setFilter(filterKey, new Set([`>=${minVal}`]));
    } else if (maxVal != null) {
      source.setFilter(filterKey, new Set([`<=${maxVal}`]));
    }
    filterOpenState = { ...filterOpenState, [slug]: false };
  }

  export function refresh() {
    source.refresh();
  }

  export function setFilter(key: string, values: Set<string>) {
    source.setFilter(key, values);
  }

  export function setCategoryFilter(id: number | null) {
    source.setFilter('category_id', id != null ? new Set([String(id)]) : new Set());
  }
</script>

{#snippet renderCategory(value: unknown, row: PartSummary)}
  {#if value && row.category_id != null}
    <span class="group flex items-center gap-1">
      <span>{value}</span>
      <button
        type="button"
        class="opacity-0 group-hover:opacity-100 rounded p-0.5 text-muted-foreground hover:text-foreground hover:bg-accent transition-opacity"
        title="Filter by this category"
        onclick={(e) => { e.stopPropagation(); source.setFilter('category_id', new Set([String(row.category_id)])); }}
      >
        <ListFilter class="h-3 w-3" />
      </button>
    </span>
  {:else}
    {value ?? '—'}
  {/if}
{/snippet}

{#snippet renderMpn(_value: unknown, row: PartSummary)}
  <PartCell
    partId={row.id}
    mpn={row.mpn}
    {teamId}
    manufacturer={row.manufacturer}
    description={row.short_description ?? row.description}
    lifecycle={row.lifecycle_status}
    imageUrl={row.image_url}
  />
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
                onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); applyParamFilter(slug, col.key, param.unit_symbol ?? undefined); } }}
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
                onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); applyParamFilter(slug, col.key, param.unit_symbol ?? undefined); } }}
                class="h-7 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
              />
            </label>
          </div>
          <div class="flex gap-2">
            <Button size="sm" class="flex-1 h-7 text-xs" onclick={() => applyParamFilter(slug, col.key, param.unit_symbol ?? undefined)}>Apply</Button>
            {#if isFiltered}
              <Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => { source.setFilter(col.key, new Set()); filterOpenState = { ...filterOpenState, [slug]: false }; }}>Clear</Button>
            {/if}
          </div>
        </Popover.Content>
      </Popover.Root>

    {:else if param?.value_type === 'enum' || param?.value_type === 'text' || param?.value_type === 'enum_set'}
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
    category_name: renderCategory,
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
        availableIds={partCategoriesQuery.data ? new Set(partCategoriesQuery.data.map(c => c.id)) : undefined}
        placeholder="All categories"
        onSelect={(id) => {
          source.setFilter('category_id', id != null ? new Set([String(id)]) : new Set());
        }}
      />
    </div>
    <StorageLocationFilterPicker
      value={storageFilter}
      locations={storageQuery.data?.items ?? []}
      onSelect={(v) => {
        source.setFilter('storage_location_id', v ? new Set([v.locationId]) : new Set());
        source.setFilter(
          'storage_cells',
          v?.cells?.length ? new Set(v.cells.map((c) => c.join(','))) : new Set(),
        );
      }}
    />
  {/snippet}
  {#snippet toolbarRight()}
    {#if canUseLists}
      <Button
        variant="outline"
        size="sm"
        onclick={() => (addToListConfirmOpen = true)}
        disabled={addingToList || source.totalRows === 0}
        title="Add every part in the current search to the active list"
      >
        <ListPlus class={cn('h-4 w-4 mr-1', addingToList && 'animate-pulse')} />
        {addingToList ? 'Adding…' : 'Add to List'}
      </Button>
    {/if}
    <Button
      variant="outline"
      size="sm"
      onclick={() => (refreshListingsConfirmOpen = true)}
      disabled={refreshingListings}
      title="Refresh stock & prices for the listings in the current search results"
    >
      <RefreshCw class={cn('h-4 w-4 mr-1', refreshingListings && 'animate-spin')} />
      {refreshingListings ? 'Queuing…' : 'Refresh Stock & Prices'}
    </Button>
    <Button
      variant="outline"
      size="sm"
      onclick={() => (refreshParamsConfirmOpen = true)}
      disabled={refreshingParams}
      title="Re-enrich parameters & metadata for the parts in the current search results"
    >
      <Sparkles class={cn('h-4 w-4 mr-1', refreshingParams && 'animate-spin')} />
      {refreshingParams ? 'Queuing…' : 'Refresh Parameters'}
    </Button>
  {/snippet}
</DataTable>

<Dialog
  bind:open={refreshListingsConfirmOpen}
  title="Refresh stock & prices?"
  confirmLabel="Refresh"
  onConfirm={doRefreshListings}
>
  {#snippet children()}
    <div class="text-sm text-muted-foreground space-y-2">
      <p>
        This refreshes stock and pricing for the distributor listings of the
        <span class="font-medium text-foreground">{source.totalRows.toLocaleString()}</span>
        part{source.totalRows === 1 ? '' : 's'} in the current search. Parts without a
        distributor integration are skipped.
      </p>
      <p>Each listing makes a live distributor API call and counts against your rate limits.</p>
    </div>
  {/snippet}
</Dialog>

<Dialog
  bind:open={refreshParamsConfirmOpen}
  title="Refresh parameters?"
  confirmLabel="Refresh"
  onConfirm={doRefreshParameters}
>
  {#snippet children()}
    <div class="text-sm text-muted-foreground space-y-2">
      <p>
        This re-enriches parameters and metadata for the
        <span class="font-medium text-foreground">{source.totalRows.toLocaleString()}</span>
        part{source.totalRows === 1 ? '' : 's'} in the current search. Parts without a
        distributor integration are skipped.
      </p>
      <p>Each part makes a live distributor API call and counts against your rate limits.</p>
    </div>
  {/snippet}
</Dialog>

<Dialog
  bind:open={addToListConfirmOpen}
  title="Add search results to list?"
  confirmLabel="Add"
  onConfirm={doAddToList}
>
  {#snippet children()}
    <div class="text-sm text-muted-foreground space-y-2">
      <p>
        Adds all
        <span class="font-medium text-foreground">{source.totalRows.toLocaleString()}</span>
        part{source.totalRows === 1 ? '' : 's'} in the current search to
        {#if activeList}
          <span class="font-medium text-foreground">“{activeList.name}”</span>
        {:else}
          a new list
        {/if}. Parts already on the list are left as-is; quantities aren't set.
      </p>
    </div>
  {/snippet}
</Dialog>

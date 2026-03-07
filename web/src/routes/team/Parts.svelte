<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { listParts, createPart, addListing, reenrichAllParts } from '$lib/api/parts.js';
  import { listCategories, listPartCategories } from '$lib/api/categories.js';
  import { listDistributorConfigs, searchDistributor } from '$lib/api/distributors.js';
  import type { DistributorSearchProduct } from '$lib/api/distributors.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import { DataTable, RemoteDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { consumePartsStockStatus } from '$lib/stores/navState.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { ApiError } from '$lib/api/client.js';
  import { Plus, RefreshCw } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  let searchValue = $state('');
  let reenriching = $state(false);
  let reenrichConfirmOpen = $state(false);
  let categoryFilter = $state<number | null>(null);
  let createOpen = $state(false);
  let createMode = $state<'manual' | 'import'>('manual');
  let duplicateFound = $state<{ id: string; mpn: string | null; manufacturer: string | null; description: string | null } | null>(null);

  // Create part — manual mode
  let newMpn = $state('');
  let newManufacturer = $state('');
  let newDescription = $state('');
  let newScope = $state<'global' | 'team'>('team');
  let creating = $state(false);

  // Create part — import from distributor mode
  let importDistributorId = $state('');
  let importKeywords = $state('');
  let importResults = $state<DistributorSearchProduct[]>([]);
  let importSearching = $state(false);
  let importSelected = $state<DistributorSearchProduct | null>(null);
  let importScope = $state<'global' | 'team'>('team');
  let importing = $state(false);

  let configsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
    enabled: createOpen && createMode === 'import',
  }));

  const enabledConfigs = $derived((configsQuery.data?.items ?? []).filter((c) => c.enabled));

  $effect(() => {
    if (createMode === 'import' && enabledConfigs.length > 0 && !importDistributorId) {
      const preferred = enabledConfigs.find((c) => c.preferred);
      importDistributorId = (preferred ?? enabledConfigs[0]).distributor_id;
    }
  });

  function resetDialog() {
    newMpn = ''; newManufacturer = ''; newDescription = ''; newScope = 'team';
    importKeywords = ''; importResults = []; importSelected = null; importDistributorId = ''; importScope = 'team';
    duplicateFound = null;
  }

  async function doImportSearch() {
    if (!importKeywords.trim() || !importDistributorId) return;
    importSelected = null;
    importSearching = true;
    try {
      const res = await searchDistributor(teamId, importDistributorId, importKeywords.trim());
      importResults = res.items;
    } catch (e: any) {
      pushToast('error', 'Search failed', e.message);
      importResults = [];
    } finally {
      importSearching = false;
    }
  }

  function onImportKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); doImportSearch(); }
  }

  // --- DataTable setup ---

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
      const stockStatus = [...(params.filters.get('stock_status') ?? [])][0] ?? undefined;
      const lifecycleStatus = [...(params.filters.get('lifecycle_status') ?? [])][0] ?? undefined;
      const categoryIdStr = [...(params.filters.get('category_id') ?? [])][0] ?? undefined;
      return listParts({
        team_id: teamId,
        q: (params.extra.q as string) || undefined,
        stock_status: stockStatus,
        lifecycle_status: lifecycleStatus,
        category_id: categoryIdStr ? Number(categoryIdStr) : undefined,
        limit: params.pageSize,
        offset: (params.page - 1) * params.pageSize,
      });
    },
    () => ({ q: searchValue }),
    { pageSize: 50 },
  );

  // Initialise stock status filter from nav state
  const initialStockStatus = consumePartsStockStatus();
  if (initialStockStatus) {
    source.setFilter('stock_status', new Set([initialStockStatus]));
  }

  async function doReenrichAll() {
    reenrichConfirmOpen = false;
    reenriching = true;
    try {
      const res = await reenrichAllParts(teamId);
      pushToast('success', 'Re-enrichment queued', `${res.job_count} part${res.job_count === 1 ? '' : 's'} queued for enrichment.`);
    } catch (err: any) {
      pushToast('error', 'Re-enrichment failed', err.message);
    } finally {
      reenriching = false;
    }
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    creating = true;
    duplicateFound = null;
    try {
      await createPart({
        scope: newScope,
        team_id: newScope === 'team' ? teamId : undefined,
        mpn: newMpn.trim() || undefined,
        manufacturer: newManufacturer.trim() || undefined,
        description: newDescription.trim() || undefined,
      });
      createOpen = false;
      resetDialog();
      source.refresh();
      pushToast('success', 'Part created');
    } catch (err: any) {
      if (err instanceof ApiError && err.code === 'PART_EXISTS') {
        duplicateFound = (err.data as any).existing_part;
      } else {
        pushToast('error', 'Failed to create part', err.message);
      }
    } finally {
      creating = false;
    }
  }

  async function handleImport(e: Event) {
    e.preventDefault();
    if (!importSelected) return;
    importing = true;
    try {
      let partId: string;
      let isExisting = false;
      try {
        const part = await createPart({
          scope: importScope,
          team_id: importScope === 'team' ? teamId : undefined,
          mpn: importSelected.mpn || undefined,
          manufacturer: importSelected.manufacturer || undefined,
          description: importSelected.description || undefined,
          category: importSelected.category || undefined,
          package: importSelected.package || undefined,
        });
        partId = part.id;
      } catch (err: any) {
        if (err instanceof ApiError && err.code === 'PART_EXISTS') {
          partId = (err.data as any).existing_part.id;
          isExisting = true;
        } else {
          throw err;
        }
      }
      await addListing(partId, teamId, {
        sku: importSelected.primary_sku,
        distributor_id: importDistributorId,
      });
      createOpen = false;
      resetDialog();
      if (isExisting) {
        pushToast('success', 'Part already exists', 'Listing data will be refreshed shortly.');
      } else {
        pushToast('success', 'Part imported', 'Parameters will populate shortly.');
      }
      navigate(`/teams/${teamId}/parts/${partId}`);
    } catch (err: any) {
      pushToast('error', 'Failed to import part', err.message);
    } finally {
      importing = false;
    }
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

<div class="flex flex-col h-full">
  <Topbar title="Parts">
  {#snippet actions()}
    {#if authStore.user?.is_admin}
      <Button size="sm" variant="outline" disabled={reenriching} onclick={() => (reenrichConfirmOpen = true)}>
        <RefreshCw class="h-4 w-4 mr-1" />
        {reenriching ? 'Enriching…' : 'Re-enrich All'}
      </Button>
    {/if}
    <Button size="sm" onclick={() => (createOpen = true)}>
      <Plus class="h-4 w-4 mr-1" /> Add Part
    </Button>
  {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    <DataTable
      {source}
      getRowKey={(p) => p.id}
      onRowClick={(p) => navigate(`/teams/${teamId}/parts/${p.id}`)}
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
  </div>
</div>

<!-- Re-enrich All Confirmation Dialog -->
<Dialog
  bind:open={reenrichConfirmOpen}
  title="Re-enrich all parts?"
  confirmLabel="Re-enrich All"
  onConfirm={doReenrichAll}
>
  {#snippet children()}
    <div class="text-sm text-muted-foreground space-y-2">
      <p>This will queue an enrichment job for every part in this team's inventory that has a distributor listing. Each job makes a live API call to the distributor.</p>
      <p>For large inventories this may take a while to process and will count against your distributor API rate limits.</p>
    </div>
  {/snippet}
</Dialog>

<!-- Create Part Dialog -->
<DialogPrimitive.Root bind:open={createOpen} onOpenChange={(o) => { if (!o) resetDialog(); }}>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <DialogPrimitive.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <DialogPrimitive.Title class="text-lg font-semibold mb-4">Add Part</DialogPrimitive.Title>

      <!-- Mode toggle -->
      <div class="flex gap-1 mb-4 border border-border rounded-md p-0.5 w-fit">
        <button
          type="button"
          class="px-3 py-1 text-sm rounded transition-colors {createMode === 'manual' ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'}"
          onclick={() => { createMode = 'manual'; importResults = []; importSelected = null; }}
        >Create manually</button>
        <button
          type="button"
          class="px-3 py-1 text-sm rounded transition-colors {createMode === 'import' ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'}"
          onclick={() => { createMode = 'import'; }}
        >Import from distributor</button>
      </div>

      {#if createMode === 'manual'}
        <form onsubmit={handleCreate} class="space-y-3">
          <div class="space-y-1">
            <Label for="scope">Scope</Label>
            <Select id="scope" bind:value={newScope}>
              <option value="team">Team-scoped</option>
              <option value="global">Global</option>
            </Select>
          </div>
          <div class="space-y-1">
            <Label for="mpn">MPN</Label>
            <Input id="mpn" bind:value={newMpn} placeholder="e.g. LM317T" oninput={() => (duplicateFound = null)} />
          </div>
          <div class="space-y-1">
            <Label for="mfr">Manufacturer</Label>
            <Input id="mfr" bind:value={newManufacturer} placeholder="e.g. Texas Instruments" oninput={() => (duplicateFound = null)} />
          </div>
          <div class="space-y-1">
            <Label for="desc">Description</Label>
            <Input id="desc" bind:value={newDescription} placeholder="Short description" />
          </div>
          {#if duplicateFound}
            <div class="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-700 dark:bg-amber-950">
              <p class="font-medium text-amber-800 dark:text-amber-200">This part already exists</p>
              <p class="mt-0.5 text-amber-700 dark:text-amber-300">
                {duplicateFound.mpn ?? ''}
                {#if duplicateFound.manufacturer} · {duplicateFound.manufacturer}{/if}
                {#if duplicateFound.description} — {duplicateFound.description}{/if}
              </p>
              <Button
                size="sm"
                class="mt-2"
                onclick={() => { const id = duplicateFound!.id; createOpen = false; resetDialog(); navigate(`/teams/${teamId}/parts/${id}`); }}
              >
                Open existing part
              </Button>
            </div>
          {/if}
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onclick={() => (createOpen = false)}>Cancel</Button>
            <Button type="submit" disabled={creating || !!duplicateFound}>{creating ? 'Creating…' : 'Create'}</Button>
          </div>
        </form>

      {:else}
        <form onsubmit={handleImport} class="space-y-3">
          <div class="space-y-1">
            <Label>Scope</Label>
            <Select bind:value={importScope}>
              <option value="team">Team-scoped</option>
              <option value="global">Global</option>
            </Select>
          </div>
          <div class="space-y-1">
            <Label>Distributor</Label>
            <Select bind:value={importDistributorId} onchange={() => { importKeywords = ''; importResults = []; importSelected = null; }}>
              {#each enabledConfigs as cfg (cfg.distributor_id)}
                <option value={cfg.distributor_id}>{cfg.distributor_name}{cfg.preferred ? ' (preferred)' : ''}</option>
              {/each}
            </Select>
          </div>
          <div class="space-y-1">
            <Label>Search</Label>
            <div class="flex gap-2">
              <Input
                placeholder="MPN, description…"
                bind:value={importKeywords}
                onkeydown={onImportKeydown}
                disabled={!importDistributorId}
                class="flex-1"
              />
              <Button type="button" variant="outline" size="sm" disabled={!importKeywords.trim() || importSearching} onclick={doImportSearch}>
                {importSearching ? 'Searching…' : 'Search'}
              </Button>
            </div>
          </div>

          {#if importResults.length > 0}
            <div class="border border-border rounded-md divide-y divide-border max-h-48 overflow-y-auto">
              {#each importResults as result (result.primary_sku)}
                {@const isSelected = importSelected?.primary_sku === result.primary_sku}
                <button
                  type="button"
                  class="w-full text-left px-3 py-2 hover:bg-accent/50 transition-colors {isSelected ? 'bg-accent' : ''}"
                  onclick={() => (importSelected = result)}
                >
                  <div class="flex items-center justify-between gap-2">
                    <div class="min-w-0">
                      <p class="font-mono text-xs font-medium truncate">{result.mpn}</p>
                      <p class="text-xs text-muted-foreground truncate">{result.manufacturer} · {result.description}</p>
                    </div>
                    <p class="shrink-0 text-xs font-mono text-muted-foreground">{result.primary_sku}</p>
                  </div>
                </button>
              {/each}
            </div>
          {/if}

          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onclick={() => (createOpen = false)}>Cancel</Button>
            <Button type="submit" disabled={!importSelected || importing}>
              {importing ? 'Importing…' : 'Import'}
            </Button>
          </div>
        </form>
      {/if}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

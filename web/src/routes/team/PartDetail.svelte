<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getPart, updatePart, getPartAllocations, type PartAllocation } from '$lib/api/parts.js';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { listCategories } from '$lib/api/categories.js';
  import type { LifecycleStatus } from '$lib/types/parts.js';
  import { createInventoryEntry, updateInventoryEntry, createAdjustment } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import AdjustmentHistory from '$lib/components/inventory/AdjustmentHistory.svelte';
  import ParameterTable from '$lib/components/parts/ParameterTable.svelte';
  import ListingTable from '$lib/components/parts/ListingTable.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { ArrowLeft, PackagePlus, Pencil } from 'lucide-svelte';

  let { teamId, partId }: { teamId: string; partId: string } = $props();

  const qc = useQueryClient();
  let tracking = $state(false);

  // Adjust stock dialog
  let adjustOpen = $state(false);
  let adjustDelta = $state('');
  let adjustReason = $state('');
  let adjusting = $state(false);

  // Edit settings dialog
  let editOpen = $state(false);
  let editStorageLocationId = $state('');
  let editMinStock = $state('');
  let editReorderQty = $state('');
  let editSaving = $state(false);

  // Edit part dialog
  let editPartOpen = $state(false);
  let editDescription = $state('');
  let editCategoryId = $state<number | null>(null);
  let editPackage = $state('');
  let editDatasheetUrl = $state('');
  let editLifecycleStatus = $state<LifecycleStatus | undefined>(undefined);
  let editPartSaving = $state(false);

  let partQuery = createQuery(() => ({
    queryKey: ['part', partId, teamId],
    queryFn: () => getPart(partId, teamId),
  }));

  let storageQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  let categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: listCategories,
    staleTime: Infinity,
  }));

  let tracked = $derived(partQuery.data?.inventory_entry_id != null);
  let entryId = $derived(partQuery.data?.inventory_entry_id ?? '');

  let allocationsQuery = createQuery(() => ({
    queryKey: ['part-allocations', partId, teamId],
    queryFn: () => getPartAllocations(partId, teamId),
    enabled: tracked,
  }));

  let totalReserved = $derived(
    allocationsQuery.data?.items.reduce((sum, a) => sum + a.quantity_reserved, 0) ?? 0
  );

  function allocStatusVariant(status: string): 'secondary' | 'default' | 'warning' | 'success' {
    switch (status) {
      case 'draft': return 'secondary';
      case 'pending': return 'default';
      case 'ordered': return 'warning';
      case 'ready': return 'success';
      default: return 'secondary';
    }
  }

  function allocStatusLabel(status: string): string {
    switch (status) {
      case 'draft': return 'Draft';
      case 'pending': return 'Pending';
      case 'ordered': return 'Ordered';
      case 'ready': return 'Ready';
      default: return status;
    }
  }

  const allocColumns: ColumnSpec<PartAllocation>[] = [
    { key: 'project_name', header: 'Project', type: 'text', sortable: true },
    {
      key: 'build_status', header: 'Status', type: 'enum', sortable: true,
      filterable: true,
      filterValues: ['draft', 'pending', 'ordered', 'ready'],
      filterLabels: { draft: 'Draft', pending: 'Pending', ordered: 'Ordered', ready: 'Ready' },
      exportValue: (row) => allocStatusLabel(row.build_status),
    },
    { key: 'build_quantity', header: 'Build Qty', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_needed', header: 'Needed', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_reserved', header: 'Reserved', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_received', header: 'Received', type: 'numeric', align: 'right', sortable: true },
  ];

  const allocSource = new LocalDataSource<PartAllocation>(
    () => allocationsQuery.data?.items ?? [],
    allocColumns,
  );

  async function handleStartTracking() {
    tracking = true;
    try {
      await createInventoryEntry(teamId, { part_id: partId });
      qc.invalidateQueries({ queryKey: ['part', partId, teamId] });
      qc.invalidateQueries({ queryKey: ['parts', teamId] });
      pushToast('success', 'Inventory tracking enabled');
    } catch (e: any) {
      pushToast('error', 'Failed to enable tracking', e.message);
    } finally {
      tracking = false;
    }
  }

  function openAdjust() {
    adjustDelta = '';
    adjustReason = '';
    adjustOpen = true;
  }

  function openEdit() {
    const part = partQuery.data;
    if (!part) return;
    editStorageLocationId = part.storage_location_id ?? '';
    editMinStock = part.minimum_stock_quantity != null ? String(part.minimum_stock_quantity) : '';
    editReorderQty = part.reorder_quantity != null ? String(part.reorder_quantity) : '';
    editOpen = true;
  }

  function openEditPart() {
    const part = partQuery.data;
    if (!part) return;
    editDescription = part.description ?? '';
    editCategoryId = part.category_id ?? null;
    editPackage = part.package ?? '';
    editDatasheetUrl = part.datasheet_url ?? '';
    editLifecycleStatus = part.lifecycle_status ?? 'active';
    editPartOpen = true;
  }

  async function handleEditPartSave(e: Event) {
    e.preventDefault();
    editPartSaving = true;
    try {
      await updatePart(partId, {
        description: editDescription || undefined,
        category_id: editCategoryId ?? undefined,
        package: editPackage || undefined,
        datasheet_url: editDatasheetUrl || undefined,
        lifecycle_status: editLifecycleStatus || undefined,
      });
      editPartOpen = false;
      qc.invalidateQueries({ queryKey: ['part', partId, teamId] });
      qc.invalidateQueries({ queryKey: ['parts', teamId] });
      pushToast('success', 'Part updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update part', err.message);
    } finally {
      editPartSaving = false;
    }
  }

  async function handleAdjust(e: Event) {
    e.preventDefault();
    const delta = parseInt(adjustDelta, 10);
    if (isNaN(delta) || delta === 0) return;
    adjusting = true;
    try {
      await createAdjustment(teamId, entryId, { delta, reason: adjustReason.trim() });
      adjustOpen = false;
      qc.invalidateQueries({ queryKey: ['part', partId, teamId] });
      qc.invalidateQueries({ queryKey: ['adjustments', teamId, entryId] });
      qc.invalidateQueries({ queryKey: ['parts', teamId] });
      pushToast('success', `Stock adjusted (${delta >= 0 ? '+' : ''}${delta})`);
    } catch (err: any) {
      pushToast('error', 'Failed to adjust stock', err.message);
    } finally {
      adjusting = false;
    }
  }

  async function handleEditSave(e: Event) {
    e.preventDefault();
    editSaving = true;
    try {
      await updateInventoryEntry(teamId, entryId, {
        storage_location_id: editStorageLocationId || null,
        minimum_stock_quantity: editMinStock ? parseInt(editMinStock, 10) : null,
        reorder_quantity: editReorderQty ? parseInt(editReorderQty, 10) : null,
      });
      editOpen = false;
      qc.invalidateQueries({ queryKey: ['part', partId, teamId] });
      pushToast('success', 'Settings updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update settings', err.message);
    } finally {
      editSaving = false;
    }
  }
</script>

{#snippet renderAllocStatus(value: unknown, _row: PartAllocation)}
  <Badge variant={allocStatusVariant(String(value))}>
    {allocStatusLabel(String(value))}
  </Badge>
{/snippet}

<div class="flex flex-col h-full">
  <Topbar
    title={partQuery.data?.mpn ?? 'Part Detail'}
    breadcrumbs={[
      { label: 'Parts', href: `/teams/${teamId}/parts` },
    ]}
  >
    {#snippet badges()}
      {#if partQuery.data}
        <LifecycleBadge status={partQuery.data.lifecycle_status} />
        <Badge variant="outline" class="capitalize">{partQuery.data.scope}</Badge>
        {#if tracked}
          <StockBadge
            quantity_on_hand={partQuery.data.quantity_on_hand ?? 0}
            minimum_stock_quantity={partQuery.data.minimum_stock_quantity}
          />
        {/if}
      {/if}
    {/snippet}
    {#snippet actions()}
      <Button variant="outline" size="sm" onclick={() => navigate(`/teams/${teamId}/parts`)}>
        <ArrowLeft class="h-4 w-4 mr-1" /> Back
      </Button>
      {#if partQuery.data}
        <Button variant="outline" size="sm" onclick={openEditPart}>
          <Pencil class="h-4 w-4 mr-1" /> Edit Part
        </Button>
      {/if}
      {#if tracked}
        <Button variant="outline" size="sm" onclick={openEdit}>Edit Settings</Button>
        <Button variant="outline" size="sm" onclick={openAdjust}>Adjust Stock</Button>
      {/if}
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if partQuery.isPending}
      <div class="space-y-3">
        <Skeleton class="h-6 w-48" />
        <Skeleton class="h-4 w-72" />
      </div>
    {:else if partQuery.data}
      {@const part = partQuery.data}
      <div class="space-y-6">
        <!-- Overview card -->
        <MetadataCard title={part.mpn ?? '—'} titleClass="font-mono" description={part.short_description ?? part.description ?? '—'}>
          <!-- Part metadata -->
          <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm">
            <div><span class="text-muted-foreground">Manufacturer:</span> <span class="font-medium">{part.manufacturer ?? '—'}</span></div>
            <div><span class="text-muted-foreground">Category:</span> <span class="font-medium">{part.category_name ?? '—'}</span>{#if part.category_source === 'user'}<span class="ml-1 text-xs text-muted-foreground" title="Manually assigned">✎</span>{/if}</div>
            <div><span class="text-muted-foreground">Package:</span> <span class="font-medium">{part.package ?? '—'}</span></div>
            {#if part.datasheet_url}
              <div><span class="text-muted-foreground">Datasheet:</span> <a href={part.datasheet_url} target="_blank" class="text-primary hover:underline">Link</a></div>
            {/if}
            {#if part.product_url}
              <div><span class="text-muted-foreground">Product Page:</span> <a href={part.product_url} target="_blank" rel="noopener noreferrer" class="text-primary hover:underline">Link</a></div>
            {/if}
          </div>
          {#if part.description && part.short_description && part.description !== part.short_description}
            <p class="text-sm text-muted-foreground">{part.description}</p>
          {/if}

          {#snippet rightFull()}
            {#if part.image_url}
              <img src={part.image_url} alt={part.mpn ?? 'Part image'} class="h-full w-64 object-contain rounded border border-border bg-white p-2" />
            {/if}
          {/snippet}

          {#snippet footer()}
            {#if tracked}
              {@const onHand = part.quantity_on_hand ?? 0}
              {@const available = onHand - totalReserved}
              <div class="grid grid-cols-3 sm:grid-cols-6 gap-4">
                <div>
                  <p class="text-xs text-muted-foreground mb-0.5">In Stock</p>
                  <p class="text-2xl font-bold tabular-nums">{onHand}</p>
                </div>
                <div>
                  <p class="text-xs text-muted-foreground mb-0.5">Reserved</p>
                  <p class="text-2xl font-bold tabular-nums {totalReserved > 0 ? 'text-warning' : 'text-muted-foreground'}">{totalReserved}</p>
                </div>
                <div>
                  <p class="text-xs text-muted-foreground mb-0.5">Available</p>
                  <p class="text-2xl font-bold tabular-nums {available < 0 ? 'text-destructive' : available === 0 && totalReserved > 0 ? 'text-muted-foreground' : ''}">{available}</p>
                </div>
                <div>
                  <p class="text-xs text-muted-foreground mb-0.5">Storage Location</p>
                  <p class="text-sm font-medium">{part.storage_location_label ?? '—'}</p>
                </div>
                <div>
                  <p class="text-xs text-muted-foreground mb-0.5">Min Stock</p>
                  <p class="text-sm font-medium">{part.minimum_stock_quantity ?? '—'}</p>
                </div>
                <div>
                  <p class="text-xs text-muted-foreground mb-0.5">Reorder Qty</p>
                  <p class="text-sm font-medium">{part.reorder_quantity ?? '—'}</p>
                </div>
              </div>
            {:else}
              <div class="flex items-center gap-3">
                <p class="text-sm text-muted-foreground">Stock not tracked for this part.</p>
                <Button size="sm" variant="outline" disabled={tracking} onclick={handleStartTracking}>
                  <PackagePlus class="h-4 w-4 mr-1" />
                  {tracking ? 'Enabling…' : 'Start Tracking'}
                </Button>
              </div>
            {/if}
          {/snippet}
        </MetadataCard>

        <!-- Aliases -->
        {#if part.aliases?.length > 0}
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Also Known As</h3>
            <div class="flex flex-wrap gap-1.5">
              {#each part.aliases as alias}
                <span class="inline-flex items-center rounded-md border border-border bg-muted px-2 py-0.5 text-xs font-mono text-muted-foreground">{alias}</span>
              {/each}
            </div>
          </section>
        {/if}

        <!-- Stacked sections -->
        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Listings</h3>
          <ListingTable
            {partId}
            {teamId}
            listings={part.listings ?? []}
            {entryId}
            preferredListingId={part.preferred_listing_id}
            onrefresh={() => qc.invalidateQueries({ queryKey: ['part', partId, teamId] })}
            onpreferredchange={() => qc.invalidateQueries({ queryKey: ['part', partId, teamId] })}
            ondelete={() => qc.invalidateQueries({ queryKey: ['part', partId, teamId] })}
            onadd={() => qc.invalidateQueries({ queryKey: ['part', partId, teamId] })}
          />
        </section>

        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Parameters</h3>
          <ParameterTable
            {partId}
            parameters={part.parameters ?? { user_specified: [], distributor_sourced: [] }}
            preferredSourceId={part.preferred_parameter_source_id}
            listings={part.listings ?? []}
            onchange={() => qc.invalidateQueries({ queryKey: ['part', partId, teamId] })}
          />
        </section>

        {#if tracked && (allocationsQuery.data?.items.length ?? 0) > 0}
          <section class="space-y-2">
            
            <DataTable
              source={allocSource}
              getRowKey={(a) => a.build_id}
              onRowClick={(a) => navigate(`/teams/${teamId}/builds/${a.build_id}`)}
              renderers={{ build_status: renderAllocStatus }}
              emptyMessage="No active reservations"
            >
              {#snippet toolbarLeft()}
                <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Reservations</h3>
              {/snippet}
            </DataTable>
          </section>
        {/if}


        {#if tracked}
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">History</h3>
            <AdjustmentHistory {entryId} {teamId} />
          </section>
        {/if}
      </div>
    {/if}
  </div>
</div>

<!-- Adjust Stock Dialog -->
<Dialog.Root bind:open={adjustOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Adjust Stock</Dialog.Title>
      <form onsubmit={handleAdjust} class="space-y-3">
        <div class="space-y-1">
          <Label for="adj-delta">Delta <span class="text-muted-foreground text-xs">(positive or negative)</span></Label>
          <Input
            id="adj-delta"
            type="number"
            bind:value={adjustDelta}
            placeholder="e.g. +10 or -5"
            required
          />
        </div>
        <div class="space-y-1">
          <Label for="adj-reason">Reason</Label>
          <Input id="adj-reason" bind:value={adjustReason} placeholder="Optional reason" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => adjustOpen = false}>Cancel</Button>
          <Button type="submit" disabled={adjusting || !adjustDelta || adjustDelta === '0'}>
            {adjusting ? 'Adjusting…' : 'Apply'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Edit Part Dialog -->
<Dialog.Root bind:open={editPartOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Edit Part</Dialog.Title>
      <form onsubmit={handleEditPartSave} class="space-y-3">
        <div class="space-y-1">
          <Label for="ep-desc">Description</Label>
          <Input id="ep-desc" bind:value={editDescription} placeholder="e.g. 10kΩ 1% 0402 resistor" />
        </div>
        <div class="space-y-1">
          <Label for="ep-category">Category</Label>
          <CategoryPicker
            value={editCategoryId}
            categories={categoriesQuery.data ?? []}
            onSelect={(id) => { editCategoryId = id; }}
          />
        </div>
        <div class="space-y-1">
          <Label for="ep-package">Package</Label>
          <Input id="ep-package" bind:value={editPackage} placeholder="e.g. 0402" />
        </div>
        <div class="space-y-1">
          <Label for="ep-datasheet">Datasheet URL</Label>
          <Input id="ep-datasheet" type="url" bind:value={editDatasheetUrl} placeholder="https://…" />
        </div>
        <div class="space-y-1">
          <Label for="ep-lifecycle">Lifecycle Status</Label>
          <Select id="ep-lifecycle" bind:value={editLifecycleStatus}>
            <option value="active">Active</option>
            <option value="nrnd">NRND</option>
            <option value="obsolete">Obsolete</option>
            <option value="deprecated">Deprecated</option>
            <option value="no_longer_available">No Longer Available</option>
          </Select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => editPartOpen = false}>Cancel</Button>
          <Button type="submit" disabled={editPartSaving}>
            {editPartSaving ? 'Saving…' : 'Save Changes'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Edit Settings Dialog -->
<Dialog.Root bind:open={editOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Edit Inventory Settings</Dialog.Title>
      <form onsubmit={handleEditSave} class="space-y-3">
        <div class="space-y-1">
          <Label for="edit-location">Storage Location</Label>
          <Select id="edit-location" bind:value={editStorageLocationId}>
            <option value="">None</option>
            {#each (storageQuery.data?.items ?? []) as loc (loc.id)}
              <option value={loc.id}>{loc.label}</option>
            {/each}
          </Select>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-1">
            <Label for="edit-min">Min Stock Qty</Label>
            <Input id="edit-min" type="number" bind:value={editMinStock} placeholder="None" min="0" />
          </div>
          <div class="space-y-1">
            <Label for="edit-reorder">Reorder Qty</Label>
            <Input id="edit-reorder" type="number" bind:value={editReorderQty} placeholder="None" min="0" />
          </div>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => editOpen = false}>Cancel</Button>
          <Button type="submit" disabled={editSaving}>
            {editSaving ? 'Saving…' : 'Save Changes'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

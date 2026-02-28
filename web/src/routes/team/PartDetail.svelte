<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getPart } from '$lib/api/parts.js';
  import { createInventoryEntry, updateInventoryEntry, createAdjustment } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import AdjustmentHistory from '$lib/components/inventory/AdjustmentHistory.svelte';
  import ParameterTable from '$lib/components/parts/ParameterTable.svelte';
  import ListingTable from '$lib/components/parts/ListingTable.svelte';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { ArrowLeft, PackagePlus } from 'lucide-svelte';

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

  let partQuery = createQuery(() => ({
    queryKey: ['part', partId, teamId],
    queryFn: () => getPart(partId, teamId),
  }));

  let storageQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  let tracked = $derived(partQuery.data?.inventory_entry_id != null);
  let entryId = $derived(partQuery.data?.inventory_entry_id ?? '');

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

<div class="flex flex-col h-full">
  <Topbar
    title={partQuery.data?.mpn ?? 'Part Detail'}
    breadcrumbs={[
      { label: 'Parts', href: `/teams/${teamId}/parts` },
      { label: partQuery.data?.mpn ?? '…' },
    ]}
  >
    {#snippet actions()}
      <Button variant="outline" size="sm" onclick={() => navigate(`/teams/${teamId}/parts`)}>
        <ArrowLeft class="h-4 w-4 mr-1" /> Back
      </Button>
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
        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          <div class="flex items-start justify-between">
            <div>
              <div class="flex items-center gap-2">
                <h2 class="text-xl font-bold font-mono">{part.mpn ?? '—'}</h2>
                <LifecycleBadge status={part.lifecycle_status} />
                <Badge variant="outline" class="capitalize">{part.scope}</Badge>
                {#if tracked}
                  <StockBadge
                    quantity_on_hand={part.quantity_on_hand ?? 0}
                    minimum_stock_quantity={part.minimum_stock_quantity}
                  />
                {/if}
              </div>
              <p class="text-muted-foreground mt-1">{part.description ?? '—'}</p>
            </div>
          </div>

          <!-- Part metadata -->
          <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm">
            <div><span class="text-muted-foreground">Manufacturer:</span> <span class="font-medium">{part.manufacturer ?? '—'}</span></div>
            <div><span class="text-muted-foreground">Category:</span> <span class="font-medium">{part.category ?? '—'}</span></div>
            <div><span class="text-muted-foreground">Package:</span> <span class="font-medium">{part.package ?? '—'}</span></div>
            {#if part.datasheet_url}
              <div><span class="text-muted-foreground">Datasheet:</span> <a href={part.datasheet_url} target="_blank" class="text-primary hover:underline">Link</a></div>
            {/if}
          </div>

          <!-- Inventory panel -->
          {#if tracked}
            <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 pt-3 border-t border-border">
              <div>
                <p class="text-xs text-muted-foreground mb-0.5">Quantity on Hand</p>
                <p class="text-2xl font-bold tabular-nums">{part.quantity_on_hand ?? 0}</p>
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
            <div class="pt-3 border-t border-border flex items-center gap-3">
              <p class="text-sm text-muted-foreground">Stock not tracked for this part.</p>
              <Button size="sm" variant="outline" disabled={tracking} onclick={handleStartTracking}>
                <PackagePlus class="h-4 w-4 mr-1" />
                {tracking ? 'Enabling…' : 'Start Tracking'}
              </Button>
            </div>
          {/if}
        </div>

        <!-- Tabs -->
        <Tabs value="parameters">
          <TabsList>
            <TabsTrigger value="parameters">Parameters</TabsTrigger>
            <TabsTrigger value="listings">Listings ({part.listings?.length ?? 0})</TabsTrigger>
            {#if tracked}
              <TabsTrigger value="history">History</TabsTrigger>
            {/if}
          </TabsList>

          <TabsContent value="parameters">
            <ParameterTable
              {partId}
              parameters={part.parameters ?? { user_specified: [], distributor_sourced: [] }}
              preferredSourceId={part.preferred_parameter_source_id}
              listings={part.listings ?? []}
              onchange={() => qc.invalidateQueries({ queryKey: ['part', partId, teamId] })}
            />
          </TabsContent>

          <TabsContent value="listings">
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
          </TabsContent>

          {#if tracked}
            <TabsContent value="history">
              <AdjustmentHistory {entryId} {teamId} />
            </TabsContent>
          {/if}
        </Tabs>
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
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={adjusting || !adjustDelta || adjustDelta === '0'}>
            {adjusting ? 'Adjusting…' : 'Apply'}
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
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={editSaving}>
            {editSaving ? 'Saving…' : 'Save Changes'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

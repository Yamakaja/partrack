<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getInventoryEntry, updateInventoryEntry, createAdjustment } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import AdjustmentHistory from '$lib/components/inventory/AdjustmentHistory.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import StorageLocationPicker from '$lib/components/storage/StorageLocationPicker.svelte';
  import type { StorageLocationValue } from '$lib/components/storage/StorageLocationPicker.svelte';
  import QRDialog from '$lib/components/common/QRDialog.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { QrCode } from 'lucide-svelte';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ApiError } from '$lib/api/client.js';
  import { generateStorageLocationQRPayload } from '$lib/utils/ipc2d.js';
  import type { StorageLocation } from '$lib/types/inventory.js';

  let { teamId, entryId }: { teamId: string; entryId: string } = $props();

  const qc = useQueryClient();

  // Adjust stock dialog
  let adjustOpen = $state(false);
  let adjustDelta = $state('');
  let adjustReason = $state('');
  let adjusting = $state(false);

  // Edit settings dialog
  let editOpen = $state(false);
  let editStorageValue = $state<StorageLocationValue | null>(null);
  let editMinStock = $state('');
  let editReorderQty = $state('');
  let editSaving = $state(false);

  // QR dialog
  let qrOpen = $state(false);

  const entryQuery = createQuery(() => ({
    queryKey: ['inventory-entry', teamId, entryId],
    queryFn: () => getInventoryEntry(teamId, entryId),
  }));

  const storageQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  const allLocations = $derived(storageQuery.data?.items ?? []);

  // ── Path label helpers ────────────────────────────────────────────────────
  const locById = $derived.by(() => {
    const m = new Map<string, StorageLocation>();
    for (const l of allLocations) m.set(l.id, l);
    return m;
  });

  function fullPathLabel(locId: string): string {
    const parts: string[] = [];
    let cur = locById.get(locId);
    while (cur) {
      parts.unshift(cur.label);
      cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
    }
    return parts.join(' / ');
  }

  function coordinateLabel(loc: StorageLocation, coord: number[]): string {
    if (!loc.grid_config || coord.length === 0) return '';
    const display = loc.grid_config.display ?? 'alphanumeric';
    const parts: string[] = [];
    let offset = 0;
    for (const level of loc.grid_config.levels) {
      if (offset >= coord.length) break;
      const dimCount = level.dims.length;
      const levelCoords = coord.slice(offset, offset + dimCount);
      offset += dimCount;
      if (level.dims.length === 1) {
        const row = levelCoords[0];
        parts.push(display === 'alphanumeric' ? String.fromCharCode(65 + row) : String(row + 1));
      } else {
        const row = levelCoords[0];
        const col = levelCoords[1] ?? 0;
        parts.push(display === 'alphanumeric'
          ? `${String.fromCharCode(65 + row)}${col + 1}`
          : `(${row + 1}, ${col + 1})`);
      }
    }
    return parts.join(' / ');
  }

  /** Full human-readable label for a location+coordinate pair. */
  const storageDisplayLabel = $derived.by(() => {
    const entry = entryQuery.data;
    if (!entry?.storage_location_id) return '—';
    const path = fullPathLabel(entry.storage_location_id);
    if (!entry.storage_coordinate || entry.storage_coordinate.length === 0) return path || entry.storage_location_label || '—';
    const loc = locById.get(entry.storage_location_id);
    if (!loc) return path || entry.storage_location_label || '—';
    const coordStr = coordinateLabel(loc, entry.storage_coordinate);
    return coordStr ? `${path} / ${coordStr}` : path;
  });

  // QR payload for current entry's storage position
  const qrPayload = $derived.by(() => {
    const entry = entryQuery.data;
    if (!entry?.storage_location_id) return '';
    return generateStorageLocationQRPayload({
      locationId: entry.storage_location_id,
      coordinate: entry.storage_coordinate,
    });
  });
  const qrFilename = $derived.by(() => {
    const entry = entryQuery.data;
    if (!entry?.storage_location_id) return 'storage-qr.png';
    const loc = locById.get(entry.storage_location_id);
    return `${(loc?.label ?? 'location').toLowerCase().replace(/\s+/g, '-')}-qr.png`;
  });

  function openAdjust() {
    adjustDelta = '';
    adjustReason = '';
    adjustOpen = true;
  }

  function openEdit() {
    const entry = entryQuery.data;
    if (!entry) return;
    editStorageValue = entry.storage_location_id
      ? { locationId: entry.storage_location_id, coordinate: entry.storage_coordinate }
      : null;
    editMinStock = entry.minimum_stock_quantity != null ? String(entry.minimum_stock_quantity) : '';
    editReorderQty = entry.reorder_quantity != null ? String(entry.reorder_quantity) : '';
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
      qc.invalidateQueries({ queryKey: ['inventory-entry', teamId, entryId] });
      qc.invalidateQueries({ queryKey: ['adjustments', teamId, entryId] });
      pushToast('success', `Stock adjusted (${delta >= 0 ? '+' : ''}${delta})`);
    } catch (err: any) {
      if (err instanceof ApiError && err.status === 422 && err.code === 'INSUFFICIENT_STOCK') {
        pushToast('error', 'Cannot adjust stock', err.message);
      } else {
        pushToast('error', 'Failed to adjust stock', err.message);
      }
    } finally {
      adjusting = false;
    }
  }

  async function handleEditSave(e: Event) {
    e.preventDefault();
    editSaving = true;
    try {
      await updateInventoryEntry(teamId, entryId, {
        storage_location_id: editStorageValue?.locationId ?? null,
        storage_coordinate: editStorageValue?.coordinate ?? null,
        minimum_stock_quantity: editMinStock ? parseInt(editMinStock, 10) : null,
        reorder_quantity: editReorderQty ? parseInt(editReorderQty, 10) : null,
      });
      editOpen = false;
      qc.invalidateQueries({ queryKey: ['inventory-entry', teamId, entryId] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
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
    title={entryQuery.data?.part_mpn ?? 'Inventory Entry'}
    breadcrumbs={[
      { label: 'Inventory', href: `/teams/${teamId}/inventory` },
    ]}
  >
    {#snippet badges()}
      {#if entryQuery.data}
        <StockBadge
          quantity_on_hand={entryQuery.data.quantity_on_hand}
          minimum_stock_quantity={entryQuery.data.minimum_stock_quantity}
        />
      {/if}
    {/snippet}
    {#snippet actions()}
      <Button variant="outline" size="sm" onclick={openEdit}>Edit Settings</Button>
      <Button size="sm" onclick={openAdjust}>Adjust Stock</Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">
    {#if entryQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if entryQuery.isError}
      <div class="text-sm text-destructive">Error: {entryQuery.error.message}</div>
    {:else if entryQuery.data}
      {@const entry = entryQuery.data}

      <!-- Part info + stock info card -->
      <MetadataCard title={entry.part_mpn ?? '—'} titleClass="font-mono" description={entry.part_description ?? undefined}>
        {#snippet footer()}
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">Quantity on Hand</p>
              <p class="text-2xl font-bold tabular-nums">{entry.quantity_on_hand}</p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">Storage Location</p>
              <div class="flex items-center gap-1.5">
                <p class="text-sm font-medium">{storageDisplayLabel}</p>
                {#if entry.storage_location_id}
                  <button
                    type="button"
                    class="text-muted-foreground hover:text-foreground transition-colors"
                    onclick={() => (qrOpen = true)}
                    title="QR code for this location"
                  >
                    <QrCode class="h-3.5 w-3.5" />
                  </button>
                {/if}
              </div>
            </div>
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">Min Stock</p>
              <p class="text-sm font-medium">{entry.minimum_stock_quantity ?? '—'}</p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">Reorder Qty</p>
              <p class="text-sm font-medium">{entry.reorder_quantity ?? '—'}</p>
            </div>
          </div>
        {/snippet}
      </MetadataCard>

      <!-- Adjustment history -->
      <AdjustmentHistory {entryId} {teamId} />
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
      <Dialog.Title class="text-lg font-semibold mb-4">Edit Settings</Dialog.Title>
      <form onsubmit={handleEditSave} class="space-y-3">
        <div class="space-y-1">
          <Label>Storage Location</Label>
          <StorageLocationPicker
            value={editStorageValue}
            locations={allLocations}
            onSelect={(v) => (editStorageValue = v)}
          />
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

<!-- Storage Location QR Dialog -->
<QRDialog
  open={qrOpen}
  onOpenChange={(v) => (qrOpen = v)}
  title="Storage Location QR Code"
  description={storageDisplayLabel}
  payload={qrPayload}
  filename={qrFilename}
/>

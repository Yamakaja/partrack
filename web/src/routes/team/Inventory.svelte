<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listInventory, createInventoryEntry } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import Pagination from '$lib/components/common/Pagination.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import PartSearch from '$lib/components/parts/PartSearch.svelte';
  import StorageLocationPicker from '$lib/components/storage/StorageLocationPicker.svelte';
  import type { StorageLocationValue } from '$lib/components/storage/StorageLocationPicker.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  // Filter state
  let q = $state('');
  let filterStorageValue = $state<StorageLocationValue | null>(null);
  let belowMinStock = $state(false);
  let limit = $state(50);
  let offset = $state(0);

  // Create entry dialog state
  let createOpen = $state(false);
  let selectedPart = $state<PartSummary | null>(null);
  let newStorageValue = $state<StorageLocationValue | null>(null);
  let newMinStock = $state('');
  let newReorderQty = $state('');
  let creating = $state(false);

  const inventoryQuery = createQuery(() => ({
    queryKey: ['inventory', teamId, q, filterStorageValue?.locationId, belowMinStock, limit, offset],
    queryFn: () => listInventory(teamId, {
      q: q || undefined,
      storage_location_id: filterStorageValue?.locationId || undefined,
      below_min_stock: belowMinStock || undefined,
      limit,
      offset,
    }),
  }));

  const storageQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  const allLocations = $derived(storageQuery.data?.items ?? []);

  // ── Full path label helpers ───────────────────────────────────────────────
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

  function storageLabel(locId: string | null, coord: number[] | null): string {
    if (!locId) return '—';
    const path = fullPathLabel(locId) || '—';
    if (!coord || coord.length === 0) return path;
    const loc = locById.get(locId);
    if (!loc) return path;
    const coordStr = coordinateLabel(loc, coord);
    return coordStr ? `${path} / ${coordStr}` : path;
  }

  function handleSearch(v: string) {
    q = v;
    offset = 0;
  }

  function handleBelowMinChange(e: Event) {
    belowMinStock = (e.target as HTMLInputElement).checked;
    offset = 0;
  }

  function openCreate() {
    selectedPart = null;
    newStorageValue = null;
    newMinStock = '';
    newReorderQty = '';
    createOpen = true;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    if (!selectedPart) return;
    creating = true;
    try {
      await createInventoryEntry(teamId, {
        part_id: selectedPart.id,
        storage_location_id: newStorageValue?.locationId || undefined,
        storage_coordinate: newStorageValue?.coordinate ?? null,
        minimum_stock_quantity: newMinStock ? parseInt(newMinStock, 10) : undefined,
        reorder_quantity: newReorderQty ? parseInt(newReorderQty, 10) : undefined,
      });
      createOpen = false;
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      pushToast('success', 'Inventory entry created');
    } catch (err: any) {
      pushToast('error', 'Failed to create entry', err.message);
    } finally {
      creating = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Inventory">
    {#snippet actions()}
      <Button size="sm" onclick={openCreate}>
        <Plus class="h-4 w-4 mr-1" /> Add Entry
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <!-- Filters -->
    <div class="flex flex-wrap items-center gap-3">
      <SearchInput
        placeholder="Search by MPN or description…"
        onchange={handleSearch}
        class="w-64"
      />
      <div class="w-56">
        <StorageLocationPicker
          value={filterStorageValue}
          locations={allLocations}
          placeholder="All locations"
          onSelect={(v) => { filterStorageValue = v; offset = 0; }}
        />
      </div>
      <label class="flex items-center gap-2 text-sm text-foreground cursor-pointer select-none">
        <input
          type="checkbox"
          checked={belowMinStock}
          onchange={handleBelowMinChange}
          class="h-4 w-4 rounded border-input"
        />
        Below min stock
      </label>
    </div>

    {#if inventoryQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if inventoryQuery.isError}
      <div class="text-sm text-destructive">Error: {inventoryQuery.error.message}</div>
    {:else if inventoryQuery.data}
      {@const data = inventoryQuery.data}
      {#if data.items.length === 0}
        <EmptyState
          title="No inventory entries"
          description={q || filterStorageValue || belowMinStock ? 'Try adjusting your filters.' : 'Add your first inventory entry to get started.'}
        />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>MPN</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Qty</TableHead>
              <TableHead>Storage Location</TableHead>
              <TableHead>Min Stock</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each data.items as entry (entry.id)}
              <TableRow
                class="cursor-pointer"
                onclick={() => navigate(`/teams/${teamId}/inventory/${entry.id}`)}
              >
                <TableCell class="font-mono font-medium text-xs">{entry.part_mpn ?? '—'}</TableCell>
                <TableCell class="max-w-xs truncate text-muted-foreground">{entry.part_description ?? '—'}</TableCell>
                <TableCell class="font-medium">{entry.quantity_on_hand}</TableCell>
                <TableCell class="text-muted-foreground text-xs">
                  {storageLabel(entry.storage_location_id, entry.storage_coordinate)}
                </TableCell>
                <TableCell class="text-muted-foreground">{entry.minimum_stock_quantity ?? '—'}</TableCell>
                <TableCell>
                  <StockBadge
                    quantity_on_hand={entry.quantity_on_hand}
                    minimum_stock_quantity={entry.minimum_stock_quantity}
                  />
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
        <Pagination
          total={data.total}
          {limit}
          {offset}
          onchange={(o) => (offset = o)}
        />
      {/if}
    {/if}
  </div>
</div>

<!-- Add Entry Dialog -->
<Dialog.Root bind:open={createOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Add Inventory Entry</Dialog.Title>
      <form onsubmit={handleCreate} class="space-y-3">
        <div class="space-y-1">
          <Label>Part <span class="text-destructive">*</span></Label>
          <PartSearch {teamId} bind:value={selectedPart} />
        </div>
        <div class="space-y-1">
          <Label>Storage Location</Label>
          <StorageLocationPicker
            value={newStorageValue}
            locations={allLocations}
            onSelect={(v) => (newStorageValue = v)}
          />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="space-y-1">
            <Label for="entry-min">Min Stock Qty</Label>
            <Input id="entry-min" type="number" bind:value={newMinStock} placeholder="0" min="0" />
          </div>
          <div class="space-y-1">
            <Label for="entry-reorder">Reorder Qty</Label>
            <Input id="entry-reorder" type="number" bind:value={newReorderQty} placeholder="0" min="0" />
          </div>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={creating || !selectedPart}>
            {creating ? 'Creating…' : 'Create'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

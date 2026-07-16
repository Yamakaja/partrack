<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Dialog } from 'bits-ui';
  import { Pencil, Trash2, Plus, QrCode, Grid3x3, ChevronRight } from 'lucide-svelte';
  import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
  import {
    listInventoryByLocation,
    updateStorageLocation,
    deleteStorageLocation,
    createStorageLocation,
  } from '$lib/api/storage.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import PartCell from '$lib/components/parts/PartCell.svelte';
  import QRDialog from '$lib/components/common/QRDialog.svelte';
  import GridVisual from './GridVisual.svelte';
  import GridConfigEditor from './GridConfigEditor.svelte';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { generateStorageLocationQRPayload } from '$lib/utils/ipc2d.js';

  interface Props {
    teamId: string;
    location: StorageLocation;
    ancestors: StorageLocation[];   // root-first, includes location itself
    allLocations: StorageLocation[];
    /** Initial grid address (for URL-driven navigation). */
    initialAddress?: number[][];
    onLocationDeleted?: () => void;
    onLocationChanged?: () => void;
    /**
     * When provided, breadcrumb / sublocation clicks call this instead of
     * navigating away (used by the split-view Storage page).
     */
    onNavigate?: (locationId: string) => void;
  }

  let {
    teamId,
    location,
    ancestors,
    allLocations,
    initialAddress = [],
    onLocationDeleted,
    onLocationChanged,
    onNavigate,
  }: Props = $props();

  const qc = useQueryClient();

  // ── Grid address + multi-select state ────────────────────────────────────
  let gridAddress = $state<number[][]>([]);
  let selectedGridCells = $state<number[][]>([]);

  // Reset both when the displayed location changes or initialAddress updates.
  $effect(() => {
    void location.id;
    gridAddress = initialAddress;
    selectedGridCells = [];
  });

  function handleGridSelect(addr: number[][]) {
    gridAddress = addr;
    selectedGridCells = []; // clear cell selection when drilling up/down
  }

  function toggleGridCell(coord: number[]) {
    const key = coord.join(',');
    const idx = selectedGridCells.findIndex((c) => c.join(',') === key);
    if (idx >= 0) {
      selectedGridCells = selectedGridCells.filter((_, i) => i !== idx);
    } else {
      selectedGridCells = [...selectedGridCells, coord];
    }
  }

  /** Applies a rectangular drag: adds or removes cells depending on mode. */
  function addDragRect(cells: number[][], mode: 'add' | 'remove') {
    if (mode === 'add') {
      const existing = new Set(selectedGridCells.map((c) => c.join(',')));
      const toAdd = cells.filter((c) => !existing.has(c.join(',')));
      if (toAdd.length > 0) selectedGridCells = [...selectedGridCells, ...toAdd];
    } else {
      const removeSet = new Set(cells.map((c) => c.join(',')));
      selectedGridCells = selectedGridCells.filter((c) => !removeSet.has(c.join(',')));
    }
  }

  /** Flat address prefix for combining with leaf cell coords → full flat coord. */
  const flatAddressPrefix = $derived(gridAddress.flat());

  /** QR coordinate: only set when exactly one cell is selected. */
  const qrForCellCoord = $derived(
    selectedGridCells.length === 1
      ? [...flatAddressPrefix, ...selectedGridCells[0]]
      : null,
  );

  // ── Navigation helper ─────────────────────────────────────────────────────
  function goToLocation(id: string) {
    if (onNavigate) onNavigate(id);
    else navigate(`/teams/${teamId}/storage/${id}`);
  }

  // ── Location map for path computation ────────────────────────────────────
  const locById = $derived.by(() => {
    const m = new Map<string, StorageLocation>();
    for (const l of allLocations) m.set(l.id, l);
    return m;
  });

  /** Compute relative path from current location down to a child location. */
  function relativePathLabel(childId: string, coord: number[] | null): string {
    if (childId === location.id) {
      if (!coord || coord.length === 0) return '—';
      return coordinateLabel(location, coord);
    }
    // Walk up from child to location, collecting labels.
    const segments: string[] = [];
    let cur = locById.get(childId);
    while (cur && cur.id !== location.id) {
      segments.unshift(cur.label);
      cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
    }
    const pathPart = segments.join(' / ');
    if (!coord || coord.length === 0) return pathPart;
    const coordPart = childId ? coordinateLabel(locById.get(childId) ?? location, coord) : '';
    return coordPart ? `${pathPart} / ${coordPart}` : pathPart;
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

  // ── Inventory query ───────────────────────────────────────────────────────
  const inventoryQuery = createQuery(() => ({
    queryKey: ['storage-inventory', teamId, location.id],
    queryFn: () => listInventoryByLocation(teamId, location.id, { limit: 200 }),
  }));

  // ── Edit dialog ───────────────────────────────────────────────────────────
  let editOpen = $state(false);
  let editLabel = $state('');
  let editDescription = $state('');
  let editGridConfig = $state<GridConfig | null>(null);
  let editSaving = $state(false);

  function openEdit() {
    editLabel = location.label;
    editDescription = location.description ?? '';
    editGridConfig = location.grid_config ?? null;
    editOpen = true;
  }

  async function handleEdit(e: Event) {
    e.preventDefault();
    if (!editLabel.trim()) return;
    editSaving = true;
    try {
      await updateStorageLocation(teamId, location.id, {
        label: editLabel.trim(),
        description: editDescription.trim() || undefined,
        grid_config: editGridConfig,
      });
      editOpen = false;
      pushToast('success', 'Location updated');
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
      onLocationChanged?.();
    } catch (err: any) {
      pushToast('error', 'Failed to update location', err.message);
    } finally {
      editSaving = false;
    }
  }

  // ── Delete dialog ─────────────────────────────────────────────────────────
  let deleteOpen = $state(false);

  async function handleDelete() {
    try {
      await deleteStorageLocation(teamId, location.id);
      pushToast('success', 'Location deleted');
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
      onLocationDeleted?.();
    } catch (err: any) {
      if (err.status === 422) {
        pushToast('error', 'Cannot delete: inventory items are stored here');
      } else {
        pushToast('error', 'Failed to delete location', err.message);
      }
    }
  }

  // ── Add child dialog ──────────────────────────────────────────────────────
  let addChildOpen = $state(false);
  let childLabel = $state('');
  let childDescription = $state('');
  let childGridConfig = $state<GridConfig | null>(null);
  let childSaving = $state(false);

  function openAddChild() {
    childLabel = '';
    childDescription = '';
    childGridConfig = null;
    addChildOpen = true;
  }

  async function handleAddChild(e: Event) {
    e.preventDefault();
    if (!childLabel.trim()) return;
    childSaving = true;
    try {
      await createStorageLocation(teamId, {
        label: childLabel.trim(),
        description: childDescription.trim() || undefined,
        parent_id: location.id,
        grid_config: childGridConfig,
      });
      addChildOpen = false;
      pushToast('success', 'Child location created');
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
      onLocationChanged?.();
    } catch (err: any) {
      pushToast('error', 'Failed to create child location', err.message);
    } finally {
      childSaving = false;
    }
  }

  // ── QR dialog ─────────────────────────────────────────────────────────────
  let qrOpen = $state(false);
  let qrCoordinate = $state<number[] | null>(null);

  const qrPayload = $derived(generateStorageLocationQRPayload({
    locationId: location.id,
    coordinate: qrCoordinate ?? undefined,
  }));

  const qrDescription = $derived.by(() => {
    const base = ancestors.map((a) => a.label).join(' / ');
    if (!qrCoordinate || qrCoordinate.length === 0) return base;
    return `${base} / ${coordinateLabel(location, qrCoordinate)}`;
  });

  const qrFilename = $derived(
    `${location.label.toLowerCase().replace(/\s+/g, '-')}-qr.png`,
  );

  function openQr(coord?: number[] | null) {
    qrCoordinate = coord ?? null;
    qrOpen = true;
  }

  // ── Children list ─────────────────────────────────────────────────────────
  const directChildren = $derived(
    allLocations.filter((l) => l.parent_id === location.id),
  );
</script>

<div class="flex flex-col gap-6 p-6 overflow-auto h-full">
  <!-- Breadcrumb + actions -->
  <div class="flex items-start justify-between gap-4">
    <div class="flex flex-col gap-1 min-w-0">
      <!-- Breadcrumb -->
      <nav class="flex items-center gap-1 text-sm text-muted-foreground flex-wrap">
        {#each ancestors as anc, i (anc.id)}
          {#if i > 0}<ChevronRight class="h-3.5 w-3.5 flex-shrink-0" />{/if}
          {#if anc.id === location.id}
            <span class="text-foreground font-medium">{anc.label}</span>
          {:else}
            <button
              class="hover:text-foreground transition-colors"
              onclick={() => goToLocation(anc.id)}
            >
              {anc.label}
            </button>
          {/if}
        {/each}
      </nav>
      {#if location.description}
        <p class="text-sm text-muted-foreground">{location.description}</p>
      {/if}
    </div>

    <!-- Action buttons -->
    <div class="flex items-center gap-1 flex-shrink-0">
      <Button variant="outline" size="sm" onclick={() => openQr()}>
        <QrCode class="h-4 w-4 mr-1" /> QR Code
      </Button>
      {#if !location.grid_config}
        <Button variant="outline" size="sm" onclick={openAddChild}>
          <Plus class="h-4 w-4 mr-1" /> Add Child
        </Button>
      {/if}
      <Button variant="ghost" size="icon" onclick={openEdit} title="Edit">
        <Pencil class="h-4 w-4" />
      </Button>
      <Button variant="ghost" size="icon" onclick={() => (deleteOpen = true)} title="Delete">
        <Trash2 class="h-4 w-4 text-destructive" />
      </Button>
    </div>
  </div>

  <!-- Grid visual (if visual node) -->
  {#if location.grid_config}
    <div class="rounded-lg border border-border p-4">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
          <Grid3x3 class="h-4 w-4 inline mr-1 -mt-0.5" />
          Layout
        </h3>
        <div class="flex items-center gap-2">
          {#if selectedGridCells.length > 0}
            <span class="text-xs text-muted-foreground">
              {selectedGridCells.length} cell{selectedGridCells.length !== 1 ? 's' : ''} selected
            </span>
            <button
              type="button"
              class="text-xs text-muted-foreground hover:text-foreground transition-colors"
              onclick={() => (selectedGridCells = [])}
            >
              Clear
            </button>
          {/if}
          {#if qrForCellCoord}
            <Button variant="outline" size="sm" onclick={() => openQr(qrForCellCoord)}>
              <QrCode class="h-3.5 w-3.5 mr-1" /> QR for Cell
            </Button>
          {/if}
        </div>
      </div>
      <GridVisual
        config={location.grid_config}
        address={gridAddress}
        onSelect={handleGridSelect}
        multiSelect={true}
        selectedCells={selectedGridCells}
        onCellToggle={toggleGridCell}
        onDragSelect={addDragRect}
      />
    </div>
  {/if}

  <!-- Direct children (for named nodes) -->
  {#if directChildren.length > 0 && !location.grid_config}
    <div>
      <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">
        Sub-locations
      </h3>
      <div class="flex flex-col gap-1">
        {#each directChildren as child (child.id)}
          <button
            class="flex items-center gap-2 px-3 py-2 rounded-md hover:bg-muted transition-colors text-left text-sm"
            onclick={() => goToLocation(child.id)}
          >
            {#if child.grid_config}
              <Grid3x3 class="h-4 w-4 text-muted-foreground flex-shrink-0" />
            {:else}
              <span class="h-4 w-4 flex-shrink-0"></span>
            {/if}
            <span class="font-medium">{child.label}</span>
            {#if child.description}
              <span class="text-muted-foreground text-xs">{child.description}</span>
            {/if}
            <ChevronRight class="h-4 w-4 text-muted-foreground ml-auto flex-shrink-0" />
          </button>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Inventory in this location and below -->
  <div>
    <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">
      Parts in this location
    </h3>
    {#if inventoryQuery.isPending}
      <p class="text-sm text-muted-foreground">Loading…</p>
    {:else if inventoryQuery.isError}
      <p class="text-sm text-destructive">Error loading inventory</p>
    {:else}
      {@const allItems = inventoryQuery.data?.items ?? []}
      {@const items = (() => {
        if (!location.grid_config || selectedGridCells.length === 0) return allItems;
        return allItems.filter((entry) => {
          if (entry.storage_location_id !== location.id) return false;
          const coord = entry.storage_coordinate;
          if (!coord || coord.length === 0) return false;
          return selectedGridCells.some((cell) => {
            const full = [...flatAddressPrefix, ...cell];
            return full.length === coord.length && full.every((v, i) => coord[i] === v);
          });
        });
      })()}
      {#if allItems.length === 0}
        <p class="text-sm text-muted-foreground">No parts stored here yet.</p>
      {:else}
        {#if selectedGridCells.length > 0 && items.length === 0}
          <p class="text-sm text-muted-foreground">No parts in selected cells.</p>
        {:else}
        <div class="rounded-md border border-border overflow-hidden">
          <table class="w-full text-sm">
            <thead class="bg-muted/50">
              <tr>
                <th class="text-left px-3 py-2 font-medium text-muted-foreground">Part</th>
                <th class="text-right px-3 py-2 font-medium text-muted-foreground">Qty</th>
                <th class="text-left px-3 py-2 font-medium text-muted-foreground">Location</th>
              </tr>
            </thead>
            <tbody>
              {#each items as entry (entry.id)}
                {@const relLoc = relativePathLabel(
                  entry.storage_location_id ?? location.id,
                  entry.storage_coordinate,
                )}
                <tr
                  class="border-t border-border hover:bg-muted/30 cursor-pointer"
                  onclick={() => navigate(`/teams/${teamId}/parts/${entry.part_id}`)}
                >
                  <td class="px-3 py-2">
                    <PartCell partId={entry.part_id} mpn={entry.part_mpn} {teamId} description={entry.part_description} />
                  </td>
                  <td class="px-3 py-2 text-right">{entry.quantity_on_hand}</td>
                  <td class="px-3 py-2 text-muted-foreground text-xs">{relLoc}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        {/if}
      {/if}
    {/if}
  </div>
</div>

<!-- Edit Dialog -->
<Dialog.Root open={editOpen} onOpenChange={(v) => (editOpen = v)}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-4xl -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg overflow-y-auto max-h-[90vh]">
      <Dialog.Title class="text-lg font-semibold mb-4">Edit Location</Dialog.Title>
      <form onsubmit={handleEdit} class="space-y-3">
        <div class="space-y-1">
          <Label for="edit-label">Label <span class="text-destructive">*</span></Label>
          <Input id="edit-label" bind:value={editLabel} required />
        </div>
        <div class="space-y-1">
          <Label for="edit-desc">Description</Label>
          <Input id="edit-desc" bind:value={editDescription} />
        </div>
        <GridConfigEditor
          initialValue={editGridConfig}
          onChange={(v) => (editGridConfig = v)}
        />
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close><Button variant="outline" type="button">Cancel</Button></Dialog.Close>
          <Button type="submit" disabled={editSaving || !editLabel.trim()}>
            {editSaving ? 'Saving…' : 'Save Changes'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Add Child Dialog -->
<Dialog.Root open={addChildOpen} onOpenChange={(v) => (addChildOpen = v)}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-4xl -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg overflow-y-auto max-h-[90vh]">
      <Dialog.Title class="text-lg font-semibold mb-4">Add Child Location</Dialog.Title>
      <p class="text-sm text-muted-foreground mb-4">Parent: <strong>{location.label}</strong></p>
      <form onsubmit={handleAddChild} class="space-y-3">
        <div class="space-y-1">
          <Label for="child-label">Label <span class="text-destructive">*</span></Label>
          <Input id="child-label" bind:value={childLabel} placeholder="e.g. Shelf A" required />
        </div>
        <div class="space-y-1">
          <Label for="child-desc">Description</Label>
          <Input id="child-desc" bind:value={childDescription} placeholder="Optional" />
        </div>
        <GridConfigEditor
          initialValue={childGridConfig}
          onChange={(v) => (childGridConfig = v)}
        />
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close><Button variant="outline" type="button">Cancel</Button></Dialog.Close>
          <Button type="submit" disabled={childSaving || !childLabel.trim()}>
            {childSaving ? 'Creating…' : 'Create'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Delete Confirm Dialog -->
<ConfirmDialog
  bind:open={deleteOpen}
  title="Delete Storage Location"
  description="This location can only be deleted if no inventory is stored there."
  confirmLabel="Delete"
  destructive
  onconfirm={handleDelete}
/>

<!-- QR Dialog -->
<QRDialog
  open={qrOpen}
  onOpenChange={(v) => (qrOpen = v)}
  title="Storage Location QR Code"
  description={qrDescription}
  payload={qrPayload}
  filename={qrFilename}
/>

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listStorageLocations, createStorageLocation } from '$lib/api/storage.js';
  import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import QRDialog from '$lib/components/common/QRDialog.svelte';
  import StorageLocationDetail from '$lib/components/storage/StorageLocationDetail.svelte';
  import GridConfigEditor from '$lib/components/storage/GridConfigEditor.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { Plus, QrCode, ChevronDown, ChevronRight, Grid3x3 } from 'lucide-svelte';
  import { generateStorageLocationQRPayload } from '$lib/utils/ipc2d.js';
  import { cn } from '$lib/utils/cn.js';

  let { teamId, locationId }: { teamId: string; locationId?: string } = $props();

  const qc = useQueryClient();

  // ── Query ─────────────────────────────────────────────────────────────────
  const query = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  const allLocations = $derived(query.data?.items ?? []);

  // ── Tree helpers ──────────────────────────────────────────────────────────
  const locById = $derived.by(() => {
    const m = new Map<string, StorageLocation>();
    for (const l of allLocations) m.set(l.id, l);
    return m;
  });

  const childrenOf = $derived.by(() => {
    const m = new Map<string | null, StorageLocation[]>();
    for (const l of allLocations) {
      const key = l.parent_id;
      if (!m.has(key)) m.set(key, []);
      m.get(key)!.push(l);
    }
    for (const [, children] of m) {
      children.sort((a, b) => a.label.localeCompare(b.label));
    }
    return m;
  });

  function ancestorsOf(locId: string): StorageLocation[] {
    const parts: StorageLocation[] = [];
    let cur = locById.get(locId);
    while (cur) {
      parts.unshift(cur);
      cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
    }
    return parts;
  }

  function fullPathLabel(locId: string): string {
    return ancestorsOf(locId).map((l) => l.label).join(' / ');
  }

  // ── Expand/collapse ───────────────────────────────────────────────────────
  let expanded = $state<Set<string>>(new Set());
  $effect(() => {
    const roots = childrenOf.get(null) ?? [];
    expanded = new Set(roots.map((l) => l.id));
  });

  function toggleExpand(id: string, e: MouseEvent) {
    e.stopPropagation();
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }

  interface FlatNode { loc: StorageLocation; depth: number }
  const flatTree = $derived.by((): FlatNode[] => {
    const result: FlatNode[] = [];
    function visit(parentId: string | null, depth: number) {
      const children = childrenOf.get(parentId) ?? [];
      for (const loc of children) {
        result.push({ loc, depth });
        if (expanded.has(loc.id)) visit(loc.id, depth + 1);
      }
    }
    visit(null, 0);
    return result;
  });

  // ── Selection ─────────────────────────────────────────────────────────────
  let selectedId = $state<string | null>(null);

  const selectedLocation = $derived(selectedId ? (locById.get(selectedId) ?? null) : null);
  const selectedAncestors = $derived(selectedId ? ancestorsOf(selectedId) : []);

  function selectLocation(loc: StorageLocation) {
    selectedId = loc.id;
    navigate(`/teams/${teamId}/storage/${loc.id}`);
    // Expand ancestors so the selected item is visible in the tree
    if (loc.parent_id) {
      const next = new Set(expanded);
      let cur = locById.get(loc.parent_id);
      while (cur) {
        next.add(cur.id);
        cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
      }
      expanded = next;
    }
  }

  // Apply the locationId prop (from the route) once the location list has loaded.
  let propLocApplied = $state<string | null>(null);
  $effect(() => {
    if (locationId && locationId !== propLocApplied && allLocations.length > 0) {
      const loc = locById.get(locationId);
      if (loc) {
        selectedId = loc.id;
        // Expand ancestors without navigating (URL is already correct)
        if (loc.parent_id) {
          const next = new Set(expanded);
          let cur = locById.get(loc.parent_id);
          while (cur) {
            next.add(cur.id);
            cur = cur.parent_id ? locById.get(cur.parent_id) : undefined;
          }
          expanded = next;
        }
      }
      propLocApplied = locationId;
    } else if (!locationId) {
      selectedId = null;
    }
  });

  function handleDetailDeleted() {
    // Move selection to parent after deletion
    const loc = selectedId ? locById.get(selectedId) : null;
    const parentId = loc?.parent_id ?? null;
    selectedId = parentId;
    if (parentId) navigate(`/teams/${teamId}/storage/${parentId}`);
    else navigate(`/teams/${teamId}/storage`);
    qc.invalidateQueries({ queryKey: ['storage', teamId] });
  }

  function handleDetailChanged() {
    qc.invalidateQueries({ queryKey: ['storage', teamId] });
  }

  // ── Add root location dialog ──────────────────────────────────────────────
  let addRootOpen = $state(false);
  let rootLabel = $state('');
  let rootDescription = $state('');
  let rootGridConfig = $state<GridConfig | null>(null);
  let rootSaving = $state(false);

  function openAddRoot() {
    rootLabel = '';
    rootDescription = '';
    rootGridConfig = null;
    addRootOpen = true;
  }

  async function handleAddRoot(e: Event) {
    e.preventDefault();
    if (!rootLabel.trim()) return;
    rootSaving = true;
    try {
      const created = await createStorageLocation(teamId, {
        label: rootLabel.trim(),
        description: rootDescription.trim() || undefined,
        grid_config: rootGridConfig,
      });
      addRootOpen = false;
      pushToast('success', 'Location created');
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
      selectedId = created.id;
      navigate(`/teams/${teamId}/storage/${created.id}`);
    } catch (err: any) {
      pushToast('error', 'Failed to create location', err.message);
    } finally {
      rootSaving = false;
    }
  }

  // ── Add child location (from tree) dialog ────────────────────────────────
  let treeAddChildParentId = $state<string | null>(null);
  let treeAddChildOpen = $state(false);
  let treeChildLabel = $state('');
  let treeChildDescription = $state('');
  let treeChildGridConfig = $state<GridConfig | null>(null);
  let treeChildSaving = $state(false);

  function openTreeAddChild(loc: StorageLocation, e: MouseEvent) {
    e.stopPropagation();
    treeAddChildParentId = loc.id;
    treeChildLabel = '';
    treeChildDescription = '';
    treeChildGridConfig = null;
    treeAddChildOpen = true;
  }

  async function handleTreeAddChild(e: Event) {
    e.preventDefault();
    if (!treeChildLabel.trim() || !treeAddChildParentId) return;
    treeChildSaving = true;
    try {
      const created = await createStorageLocation(teamId, {
        label: treeChildLabel.trim(),
        description: treeChildDescription.trim() || undefined,
        parent_id: treeAddChildParentId,
        grid_config: treeChildGridConfig,
      });
      treeAddChildOpen = false;
      pushToast('success', 'Location created');
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
      selectedId = created.id;
      navigate(`/teams/${teamId}/storage/${created.id}`);
    } catch (err: any) {
      pushToast('error', 'Failed to create location', err.message);
    } finally {
      treeChildSaving = false;
    }
  }

  // ── Tree QR dialog ────────────────────────────────────────────────────────
  let treeQrOpen = $state(false);
  let treeQrLocation = $state<StorageLocation | null>(null);

  function openTreeQr(loc: StorageLocation, e: MouseEvent) {
    e.stopPropagation();
    treeQrLocation = loc;
    treeQrOpen = true;
  }

  const treeQrPayload = $derived(
    treeQrLocation
      ? generateStorageLocationQRPayload({ locationId: treeQrLocation.id })
      : '',
  );
  const treeQrDescription = $derived(
    treeQrLocation ? fullPathLabel(treeQrLocation.id) : '',
  );
  const treeQrFilename = $derived(
    treeQrLocation
      ? `${treeQrLocation.label.toLowerCase().replace(/\s+/g, '-')}-qr.png`
      : 'location-qr.png',
  );
</script>

<div class="flex flex-col h-full">
  <Topbar title="Storage Locations" />

  <div class="flex flex-1 overflow-hidden">
    <!-- ── Left panel: location tree ─────────────────────────────────────── -->
    <div class="w-72 border-r border-border flex flex-col overflow-hidden flex-shrink-0">
      <!-- Panel header -->
      <div class="flex items-center justify-between px-3 py-2 border-b border-border flex-shrink-0">
        <span class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
          Locations
        </span>
        <Button size="icon" variant="ghost" class="h-7 w-7" onclick={openAddRoot} title="Add root location">
          <Plus class="h-4 w-4" />
        </Button>
      </div>

      <!-- Tree -->
      <div class="flex-1 overflow-y-auto py-1">
        {#if query.isPending}
          <p class="px-3 py-2 text-sm text-muted-foreground">Loading…</p>
        {:else if query.isError}
          <p class="px-3 py-2 text-sm text-destructive">Error loading locations</p>
        {:else if flatTree.length === 0}
          <div class="px-3 py-4 text-center">
            <p class="text-sm text-muted-foreground">No locations yet</p>
            <button
              class="mt-1 text-sm text-primary underline-offset-4 hover:underline"
              onclick={openAddRoot}
            >
              Add first location
            </button>
          </div>
        {:else}
          {#each flatTree as { loc, depth } (loc.id)}
            {@const children = childrenOf.get(loc.id) ?? []}
            {@const hasChildren = children.length > 0 && !loc.grid_config}
            {@const isExpanded = expanded.has(loc.id)}
            {@const isSelected = selectedId === loc.id}
            <div class={cn('group flex w-full items-center', isSelected && 'bg-accent')}>
              <!-- Expand/collapse button or indent spacer -->
              {#if hasChildren}
                <button
                  type="button"
                  style="padding-left: {0.25 + depth * 1.125}rem"
                  class="flex-shrink-0 p-1.5 text-muted-foreground hover:text-foreground transition-colors"
                  onclick={(e) => toggleExpand(loc.id, e)}
                  aria-label={isExpanded ? 'Collapse' : 'Expand'}
                >
                  {#if isExpanded}
                    <ChevronDown class="h-3.5 w-3.5" />
                  {:else}
                    <ChevronRight class="h-3.5 w-3.5" />
                  {/if}
                </button>
              {:else}
                <!-- Spacer matching chevron button width -->
                <span
                  style="padding-left: {0.25 + depth * 1.125 + 1.5}rem"
                  class="flex-shrink-0"
                ></span>
              {/if}

              <!-- Label button (selects the location) -->
              <button
                type="button"
                class="flex flex-1 items-center gap-1.5 py-1.5 pr-1 text-sm text-left min-w-0"
                onclick={() => selectLocation(loc)}
              >
                {#if loc.grid_config}
                  <Grid3x3 class="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
                {/if}
                <span class="truncate">{loc.label}</span>
              </button>

              <!-- Hover-revealed actions -->
              <div
                class="flex items-center gap-0.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity pr-1"
              >
                {#if !loc.grid_config}
                  <button
                    type="button"
                    class="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                    onclick={(e) => openTreeAddChild(loc, e)}
                    title="Add child location"
                  >
                    <Plus class="h-3.5 w-3.5" />
                  </button>
                {/if}
                <button
                  type="button"
                  class="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                  onclick={(e) => openTreeQr(loc, e)}
                  title="QR code"
                >
                  <QrCode class="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          {/each}
        {/if}
      </div>
    </div>

    <!-- ── Right panel: detail view ───────────────────────────────────────── -->
    <div class="flex-1 overflow-hidden">
      {#if selectedLocation}
        <StorageLocationDetail
          {teamId}
          location={selectedLocation}
          ancestors={selectedAncestors}
          allLocations={allLocations}
          onLocationDeleted={handleDetailDeleted}
          onLocationChanged={handleDetailChanged}
          onNavigate={(id) => {
            const loc = locById.get(id);
            if (loc) selectLocation(loc);
          }}
        />
      {:else}
        <div class="flex h-full items-center justify-center">
          <div class="text-center text-sm text-muted-foreground space-y-1">
            <p>Select a location to view details</p>
            {#if allLocations.length === 0 && !query.isPending}
              <p>
                or
                <button
                  class="underline-offset-4 hover:underline text-primary"
                  onclick={openAddRoot}
                >
                  add your first location
                </button>
              </p>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  </div>
</div>

<!-- Add Root Location Dialog -->
<Dialog.Root open={addRootOpen} onOpenChange={(v) => (addRootOpen = v)}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-2xl -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg overflow-y-auto max-h-[90vh]">
      <Dialog.Title class="text-lg font-semibold mb-4">Add Storage Location</Dialog.Title>
      <form onsubmit={handleAddRoot} class="space-y-3">
        <div class="space-y-1">
          <Label for="root-label">Label <span class="text-destructive">*</span></Label>
          <Input id="root-label" bind:value={rootLabel} placeholder="e.g. East Wall" required />
        </div>
        <div class="space-y-1">
          <Label for="root-desc">Description</Label>
          <Input id="root-desc" bind:value={rootDescription} placeholder="Optional description" />
        </div>
        <GridConfigEditor
          initialValue={rootGridConfig}
          onChange={(v) => (rootGridConfig = v)}
        />
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={rootSaving || !rootLabel.trim()}>
            {rootSaving ? 'Creating…' : 'Create'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Add Child Location Dialog (from tree) -->
<Dialog.Root open={treeAddChildOpen} onOpenChange={(v) => (treeAddChildOpen = v)}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-2xl -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg overflow-y-auto max-h-[90vh]">
      <Dialog.Title class="text-lg font-semibold mb-1">Add Child Location</Dialog.Title>
      {#if treeAddChildParentId}
        <p class="text-sm text-muted-foreground mb-4">
          Parent: <strong>{locById.get(treeAddChildParentId)?.label ?? '—'}</strong>
        </p>
      {/if}
      <form onsubmit={handleTreeAddChild} class="space-y-3">
        <div class="space-y-1">
          <Label for="tree-child-label">Label <span class="text-destructive">*</span></Label>
          <Input id="tree-child-label" bind:value={treeChildLabel} placeholder="e.g. Shelf A" required />
        </div>
        <div class="space-y-1">
          <Label for="tree-child-desc">Description</Label>
          <Input id="tree-child-desc" bind:value={treeChildDescription} placeholder="Optional" />
        </div>
        <GridConfigEditor
          initialValue={treeChildGridConfig}
          onChange={(v) => (treeChildGridConfig = v)}
        />
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={treeChildSaving || !treeChildLabel.trim()}>
            {treeChildSaving ? 'Creating…' : 'Create'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Tree QR Dialog -->
<QRDialog
  open={treeQrOpen}
  onOpenChange={(v) => (treeQrOpen = v)}
  title="Storage Location QR Code"
  description={treeQrDescription}
  payload={treeQrPayload}
  filename={treeQrFilename}
/>

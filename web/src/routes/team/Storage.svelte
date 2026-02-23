<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listStorageLocations, createStorageLocation, updateStorageLocation, deleteStorageLocation } from '$lib/api/storage.js';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus, Pencil, Trash2 } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  // Dialog state
  let formOpen = $state(false);
  let editTarget = $state<StorageLocation | null>(null);
  let formLabel = $state('');
  let formDescription = $state('');
  let saving = $state(false);

  // Delete state
  let deleteOpen = $state(false);
  let deleteTarget = $state<StorageLocation | null>(null);

  let query = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  function openCreate() {
    editTarget = null;
    formLabel = '';
    formDescription = '';
    formOpen = true;
  }

  function openEdit(loc: StorageLocation) {
    editTarget = loc;
    formLabel = loc.label;
    formDescription = loc.description ?? '';
    formOpen = true;
  }

  function openDelete(loc: StorageLocation) {
    deleteTarget = loc;
    deleteOpen = true;
  }

  async function handleSave(e: Event) {
    e.preventDefault();
    if (!formLabel.trim()) return;
    saving = true;
    try {
      if (editTarget) {
        await updateStorageLocation(teamId, editTarget.id, {
          label: formLabel.trim(),
          description: formDescription.trim() || undefined,
        });
        pushToast('success', 'Location updated');
      } else {
        await createStorageLocation(teamId, formLabel.trim(), formDescription.trim() || undefined);
        pushToast('success', 'Location created');
      }
      formOpen = false;
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
    } catch (err: any) {
      pushToast('error', editTarget ? 'Failed to update location' : 'Failed to create location', err.message);
    } finally {
      saving = false;
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    const id = deleteTarget.id;
    try {
      await deleteStorageLocation(teamId, id);
      pushToast('success', 'Location deleted');
      qc.invalidateQueries({ queryKey: ['storage', teamId] });
    } catch (err: any) {
      if (err.status === 422 || (err.message && err.message.includes('422'))) {
        pushToast('error', 'Cannot delete: inventory items are stored here');
      } else {
        pushToast('error', 'Failed to delete location', err.message);
      }
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Storage Locations">
    {#snippet actions()}
      <Button size="sm" onclick={openCreate}>
        <Plus class="h-4 w-4 mr-1" /> Add Location
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if query.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if query.isError}
      <div class="text-sm text-destructive">Error: {query.error.message}</div>
    {:else if query.data}
      {@const items = query.data.items}
      {#if items.length === 0}
        <EmptyState
          title="No storage locations yet"
          description="Add storage locations to organise your inventory."
        >
          {#snippet action()}
            <Button size="sm" onclick={openCreate}>
              <Plus class="h-4 w-4 mr-1" /> Add Location
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Label</TableHead>
              <TableHead>Description</TableHead>
              <TableHead class="w-24">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each items as loc (loc.id)}
              <TableRow>
                <TableCell class="font-medium">{loc.label}</TableCell>
                <TableCell class="text-muted-foreground">{loc.description ?? '—'}</TableCell>
                <TableCell>
                  <div class="flex items-center gap-1">
                    <Button variant="ghost" size="icon" onclick={() => openEdit(loc)}>
                      <Pencil class="h-4 w-4" />
                    </Button>
                    <Button variant="ghost" size="icon" onclick={() => openDelete(loc)}>
                      <Trash2 class="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>

<!-- Create / Edit Dialog -->
<Dialog.Root bind:open={formOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">
        {editTarget ? 'Edit Location' : 'Add Location'}
      </Dialog.Title>
      <form onsubmit={handleSave} class="space-y-3">
        <div class="space-y-1">
          <Label for="loc-label">Label <span class="text-destructive">*</span></Label>
          <Input id="loc-label" bind:value={formLabel} placeholder="e.g. Shelf A3" required />
        </div>
        <div class="space-y-1">
          <Label for="loc-desc">Description</Label>
          <Input id="loc-desc" bind:value={formDescription} placeholder="Optional description" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={saving || !formLabel.trim()}>
            {saving ? 'Saving…' : editTarget ? 'Save Changes' : 'Create'}
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

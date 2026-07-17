<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import StorageLocationPicker from '$lib/components/storage/StorageLocationPicker.svelte';
  import type { StorageLocationValue } from '$lib/components/storage/StorageLocationPicker.svelte';
  import { listStorageLocations } from '$lib/api/storage.js';
  import { bulkAssignStorage } from '$lib/api/inventory.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import type { StorageLocation } from '$lib/types/inventory.js';

  // "Assign storage…" sink verb: one location (and optional grid coordinate)
  // for every part on the list. Parts the team doesn't track yet get a
  // zero-quantity inventory entry — entry ≠ stock.
  let {
    teamId,
    listName,
    partIds,
    open = $bindable(false),
    oncomplete,
  }: {
    teamId: string;
    listName: string;
    partIds: string[];
    open?: boolean;
    oncomplete: () => void;
  } = $props();

  let locations = $state<StorageLocation[]>([]);
  let value = $state<StorageLocationValue | null>(null);
  let submitting = $state(false);

  $effect(() => {
    if (open) {
      value = null;
      listStorageLocations(teamId)
        .then((r) => (locations = r.items))
        .catch(() => (locations = []));
    }
  });

  async function submit() {
    if (!value || submitting) return;
    submitting = true;
    try {
      const res = await bulkAssignStorage(teamId, {
        part_ids: partIds,
        storage_location_id: value.locationId,
        storage_coordinate: value.coordinate ?? undefined,
      });
      const label = locations.find((l) => l.id === value?.locationId)?.label ?? 'the location';
      pushToast(
        'success',
        `Assigned ${res.assigned} part${res.assigned === 1 ? '' : 's'} to ${label}`,
        res.created > 0 ? `${res.created} new zero-stock inventory ${res.created === 1 ? 'entry' : 'entries'} created` : undefined,
      );
      open = false;
      oncomplete();
    } catch (err) {
      pushToast('error', err instanceof Error ? err.message : 'Assignment failed');
    } finally {
      submitting = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="mb-2 text-lg font-semibold">Assign storage for “{listName}”</Dialog.Title>
      <p class="text-sm text-muted-foreground">
        Sets the preferred storage location for all
        <span class="font-semibold tabular-nums text-foreground">{partIds.length}</span>
        part{partIds.length === 1 ? '' : 's'} on the list. Parts without an inventory entry get one with zero stock.
      </p>

      <div class="mt-4">
        <StorageLocationPicker
          {value}
          {locations}
          onSelect={(v) => (value = v)}
          placeholder="Select location…"
        />
      </div>

      <div class="mt-4 flex justify-end gap-2">
        <Dialog.Close>
          <Button variant="outline" type="button">Cancel</Button>
        </Dialog.Close>
        <Button onclick={submit} disabled={!value || submitting}>
          {submitting ? 'Assigning…' : `Assign ${partIds.length} part${partIds.length === 1 ? '' : 's'}`}
        </Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

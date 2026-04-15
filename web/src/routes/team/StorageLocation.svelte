<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { getStorageLocation, listStorageLocations } from '$lib/api/storage.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import StorageLocationDetail from '$lib/components/storage/StorageLocationDetail.svelte';
  import { navigate } from '$lib/router/index.svelte.js';

  import { currentRoute } from '$lib/router/index.svelte.js';

  interface Props {
    teamId: string;
    locationId: string;
  }

  let { teamId, locationId }: Props = $props();

  // Parse ?at= query param from the URL into a flat coordinate.
  const flatCoord = $derived.by((): number[] | null => {
    // Access currentRoute() so this re-evaluates on navigation.
    void currentRoute();
    const raw = new URLSearchParams(window.location.search).get('at');
    if (!raw) return null;
    const parts = raw.split(',').map(Number);
    if (parts.some(isNaN)) return null;
    return parts;
  });

  const locationQuery = createQuery(() => ({
    queryKey: ['storage-location', teamId, locationId],
    queryFn: () => getStorageLocation(teamId, locationId),
  }));

  const allQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
  }));

  const location = $derived(locationQuery.data?.location ?? null);
  const ancestors = $derived(locationQuery.data?.ancestors ?? []);
  const allLocations = $derived(allQuery.data?.items ?? []);

  /** Reconstruct the per-level address from a flat coordinate using grid_config dims. */
  const initialAddress = $derived.by((): number[][] => {
    if (!flatCoord || !location?.grid_config) return [];
    const addr: number[][] = [];
    let offset = 0;
    for (const level of location.grid_config.levels) {
      if (offset >= flatCoord.length) break;
      const dimCount = level.dims.length;
      addr.push(flatCoord.slice(offset, offset + dimCount));
      offset += dimCount;
    }
    return addr;
  });

  function handleDeleted() {
    // Navigate to parent or storage list after deletion
    const parent = ancestors.length >= 2 ? ancestors[ancestors.length - 2] : null;
    if (parent) {
      navigate(`/teams/${teamId}/storage/${parent.id}`);
    } else {
      navigate(`/teams/${teamId}/storage`);
    }
  }

  function handleChanged() {
    // Queries auto-invalidated by the detail component
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Storage Location" />

  <div class="flex-1 overflow-hidden">
    {#if locationQuery.isPending || allQuery.isPending}
      <div class="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    {:else if locationQuery.isError}
      <div class="flex h-full items-center justify-center text-sm text-destructive">
        {locationQuery.error.message}
      </div>
    {:else if location}
      <StorageLocationDetail
        {teamId}
        {location}
        {ancestors}
        {allLocations}
        {initialAddress}
        onLocationDeleted={handleDeleted}
        onLocationChanged={handleChanged}
      />
    {/if}
  </div>
</div>

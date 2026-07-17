<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Check, ListPlus } from 'lucide-svelte';
  import { listCollections, createCollection, addCollectionItem } from '$lib/api/collections.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { listPanelStore, setActiveList } from '$lib/stores/listPanel.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Button } from '$lib/components/ui/button/index.js';

  // Adds a single part to the panel's active list (creating one if none exists),
  // the full-button sibling of PartHoverCard's ListPlus affordance. Renders
  // nothing for viewers or when the route team isn't the active team, so it can
  // be dropped into any part surface unconditionally.
  let { teamId, partId }: { teamId: string; partId: string } = $props();

  const qc = useQueryClient();

  const canUseLists = $derived(
    !!teamId &&
      activeTeamStore.current?.teamId === teamId &&
      (activeTeamStore.current?.role ?? 'viewer') !== 'viewer',
  );

  const collectionsQuery = createQuery(() => ({
    queryKey: ['collections', teamId],
    queryFn: () => listCollections(teamId),
    enabled: canUseLists,
    staleTime: 30_000,
  }));
  const activeList = $derived(
    collectionsQuery.data?.collections.find((c) => c.id === listPanelStore.activeListId) ?? null,
  );

  let busy = $state(false);
  let justAdded = $state(false);

  async function add() {
    if (busy) return;
    busy = true;
    try {
      let listId = listPanelStore.activeListId;
      let name = activeList?.name;
      if (!listId) {
        const created = await createCollection(teamId);
        setActiveList(created.id);
        listId = created.id;
        name = created.name;
      }
      await addCollectionItem(teamId, listId, { part_id: partId });
      qc.invalidateQueries({ queryKey: ['collections', teamId] });
      qc.invalidateQueries({ queryKey: ['collection', teamId, listId] });
      justAdded = true;
      setTimeout(() => (justAdded = false), 1500);
      pushToast('success', `Added to “${name ?? 'list'}”`);
    } catch (err) {
      pushToast('error', err instanceof Error ? err.message : 'Could not add to list');
    } finally {
      busy = false;
    }
  }
</script>

{#if canUseLists}
  <Button
    variant="outline"
    size="sm"
    onclick={add}
    disabled={busy}
    title={activeList ? `Add to “${activeList.name}”` : 'Add to a new list'}
  >
    {#if justAdded}
      <Check class="h-4 w-4 mr-1 text-success" /> Added
    {:else}
      <ListPlus class="h-4 w-4 mr-1" /> Add to list
    {/if}
  </Button>
{/if}

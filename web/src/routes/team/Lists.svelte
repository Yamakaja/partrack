<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Plus } from 'lucide-svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import {
    listCollections, createCollection, updateCollection, deleteCollection,
    type PartCollectionSummary,
  } from '$lib/api/collections.js';
  import { setActiveList, setListPanelOpen } from '$lib/stores/listPanel.svelte.js';

  // Management surface for lists — browse (incl. archived), unarchive, delete.
  // Day-to-day list work happens in the floating panel; this page exists so
  // archived lists stay reachable.
  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  const query = createQuery(() => ({
    queryKey: ['collections', teamId, 'all'],
    queryFn: () => listCollections(teamId, true),
    enabled: !!teamId,
  }));
  const lists = $derived(query.data?.collections ?? []);

  let deleteTarget = $state<PartCollectionSummary | null>(null);
  let deleteConfirmOpen = $state(false);

  function invalidate() {
    qc.invalidateQueries({ queryKey: ['collections', teamId] });
  }

  async function newList() {
    const c = await createCollection(teamId);
    setActiveList(c.id);
    setListPanelOpen(true);
    invalidate();
  }

  function openInPanel(c: PartCollectionSummary) {
    setActiveList(c.id);
    setListPanelOpen(true);
  }

  async function setArchived(c: PartCollectionSummary, archived: boolean) {
    await updateCollection(teamId, c.id, { archived });
    invalidate();
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    await deleteCollection(teamId, deleteTarget.id);
    deleteTarget = null;
    invalidate();
  }

  function fmtDate(iso: string): string {
    return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Lists">
    {#snippet actions()}
      <Button size="sm" onclick={newList}>
        <Plus class="h-4 w-4 mr-1" /> New List
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">
    {#if query.isPending}
      <p class="text-sm text-muted-foreground">Loading…</p>
    {:else if lists.length === 0}
      <EmptyState
        title="No lists yet"
        description="A list is a private scratchpad of parts — jot down a pile on your bench, then check it out or add it to the cart."
      >
        {#snippet action()}
          <Button size="sm" onclick={newList}>
            <Plus class="h-4 w-4 mr-1" /> New List
          </Button>
        {/snippet}
      </EmptyState>
    {:else}
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-border text-left text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            <th class="px-3 py-2">Name</th>
            <th class="px-3 py-2 text-right">Parts</th>
            <th class="px-3 py-2 text-right">Checked</th>
            <th class="px-3 py-2 text-right">Pieces</th>
            <th class="px-3 py-2">Updated</th>
            <th class="px-3 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {#each lists as c (c.id)}
            <tr class="border-b border-border {c.archived ? 'opacity-60' : ''}">
              <td class="px-3 py-2">
                <button class="font-medium hover:underline" onclick={() => openInPanel(c)}>{c.name}</button>
                {#if c.archived}
                  <Badge variant="outline" class="ml-2">archived</Badge>
                {/if}
              </td>
              <td class="px-3 py-2 text-right tabular-nums">{c.item_count}</td>
              <!-- Checked rows are a persisted selection, so this is real
                   progress: what a phone pick run has ticked off so far. -->
              <td class="px-3 py-2 text-right tabular-nums">
                {#if c.checked_count > 0}
                  {c.checked_count}/{c.item_count}
                {:else}
                  <span class="text-muted-foreground">—</span>
                {/if}
              </td>
              <td class="px-3 py-2 text-right tabular-nums">
                {c.total_quantity > 0 ? c.total_quantity.toLocaleString() : '—'}
                {#if c.unquantified_count > 0}
                  <span class="ml-1 text-[11px] text-warning">({c.unquantified_count} w/o qty)</span>
                {/if}
              </td>
              <td class="px-3 py-2 text-muted-foreground">{fmtDate(c.updated_at)}</td>
              <td class="px-3 py-2 text-right whitespace-nowrap">
                {#if c.archived}
                  <Button variant="outline" size="sm" onclick={() => setArchived(c, false)}>Unarchive</Button>
                {:else}
                  <Button variant="outline" size="sm" onclick={() => setArchived(c, true)}>Archive</Button>
                {/if}
                <Button
                  variant="outline" size="sm" class="ml-1 text-destructive"
                  onclick={() => { deleteTarget = c; deleteConfirmOpen = true; }}
                >
                  Delete
                </Button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>

<ConfirmDialog
  bind:open={deleteConfirmOpen}
  title="Delete this list?"
  description={`“${deleteTarget?.name ?? ''}” and its rows are removed permanently. Parts and stock are not affected.`}
  confirmLabel="Delete"
  destructive
  onconfirm={confirmDelete}
/>

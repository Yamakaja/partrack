<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Dialog } from 'bits-ui';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { ArrowUp, ArrowDown, Trash2, Download, X } from 'lucide-svelte';
  import PartsTable from '$lib/components/parts/PartsTable.svelte';
  import type { BOMLineItem } from '$lib/types/projects.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import {
    listLineItemSubstitutes,
    addSubstitute,
    removeSubstitute,
    reorderSubstitutes,
    importSubstitutes,
    listPreviousRevisionsWithSubstitutes,
  } from '$lib/api/revisions.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

  let {
    teamId,
    projectId,
    revisionId,
    lineItem,
    open = $bindable(false),
  }: {
    teamId: string;
    projectId: string;
    revisionId: string;
    lineItem: BOMLineItem;
    open?: boolean;
  } = $props();

  const queryClient = useQueryClient();

  const substitutesQuery = createQuery(() => ({
    queryKey: ['revision', revisionId, 'item', lineItem.id, 'substitutes'],
    queryFn: () => listLineItemSubstitutes(teamId, projectId, revisionId, lineItem.id),
    enabled: open,
  }));

  const previousRevisionsQuery = createQuery(() => ({
    queryKey: ['revision', revisionId, 'item', lineItem.id, 'previous-revisions'],
    queryFn: () => listPreviousRevisionsWithSubstitutes(teamId, projectId, revisionId, lineItem.id),
    enabled: open,
  }));

  let selectedImportRevId = $state('');
  let importing = $state(false);
  let removing = $state<Record<string, boolean>>({});
  let reordering = $state(false);

  function invalidate() {
    queryClient.invalidateQueries({ queryKey: ['revision', revisionId, 'item', lineItem.id, 'substitutes'] });
    queryClient.invalidateQueries({ queryKey: ['revision', teamId, projectId, revisionId] });
  }

  async function handleAddPart(part: PartSummary) {
    const items = substitutesQuery.data?.items ?? [];
    if (part.id === lineItem.resolved_part_id) {
      pushToast('warning', 'Cannot add the primary part as a substitute.');
      return;
    }
    if (items.some(s => s.part_id === part.id)) {
      pushToast('info', 'Part is already a substitute for this line item.');
      return;
    }
    try {
      await addSubstitute(teamId, projectId, revisionId, lineItem.id, part.id);
      invalidate();
      pushToast('success', `Added ${part.mpn ?? part.id} as substitute.`);
    } catch (err: any) {
      pushToast('error', 'Failed to add substitute.', err?.message);
    }
  }

  async function handleRemove(partId: string) {
    removing[partId] = true;
    try {
      await removeSubstitute(teamId, projectId, revisionId, lineItem.id, partId);
      invalidate();
    } catch (err: any) {
      pushToast('error', 'Failed to remove substitute.', err?.message);
    } finally {
      removing[partId] = false;
    }
  }

  async function handleMove(partId: string, direction: 'up' | 'down') {
    const items = substitutesQuery.data?.items ?? [];
    const idx = items.findIndex(s => s.part_id === partId);
    if (idx === -1) return;
    const newItems = [...items];
    const swapWith = direction === 'up' ? idx - 1 : idx + 1;
    if (swapWith < 0 || swapWith >= newItems.length) return;
    [newItems[idx], newItems[swapWith]] = [newItems[swapWith], newItems[idx]];
    reordering = true;
    try {
      await reorderSubstitutes(teamId, projectId, revisionId, lineItem.id, newItems.map(s => s.part_id));
      invalidate();
    } catch (err: any) {
      pushToast('error', 'Failed to reorder substitutes.', err?.message);
    } finally {
      reordering = false;
    }
  }

  async function handleImport() {
    if (!selectedImportRevId) return;
    importing = true;
    try {
      await importSubstitutes(teamId, projectId, revisionId, lineItem.id, selectedImportRevId);
      invalidate();
      pushToast('success', 'Substitutes imported.');
    } catch (err: any) {
      pushToast('error', 'Failed to import substitutes.', err?.message);
    } finally {
      importing = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex w-[90vw] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
      style="height: min(85vh, 720px);"
    >
      <!-- Fixed header -->
      <div class="shrink-0 border-b border-border px-6 pb-4 pt-5">
        <div class="flex items-start justify-between gap-4">
          <div class="space-y-0.5">
            <Dialog.Title class="text-lg font-semibold">Manage Substitutes</Dialog.Title>
            <p class="text-sm text-muted-foreground">
              <span class="font-mono">{lineItem.reference_designators}</span>
              {#if lineItem.resolved_part_mpn}
                — primary: <span class="font-mono">{lineItem.resolved_part_mpn}</span>
              {/if}
            </p>
          </div>
          <Dialog.Close class="rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 mt-0.5">
            <X class="h-4 w-4" />
            <span class="sr-only">Close</span>
          </Dialog.Close>
        </div>
      </div>

      <!-- Tabs: list is fixed, content area scrolls -->
      <Tabs value="current" class="flex flex-col flex-1 min-h-0">
        <TabsList class="shrink-0 mx-6 mt-4 mb-0 self-start">
          <TabsTrigger value="current">
            Current
            {#if substitutesQuery.data && substitutesQuery.data.items.length > 0}
              <Badge variant="secondary" class="ml-1.5 text-xs">{substitutesQuery.data.items.length}</Badge>
            {/if}
          </TabsTrigger>
          <TabsTrigger value="import">Import from Previous</TabsTrigger>
          <TabsTrigger value="catalog">Add from Catalog</TabsTrigger>
        </TabsList>

        <!-- Tab 1: Current substitutes -->
        <TabsContent value="current" class="mt-0 flex-1 min-h-0 overflow-auto px-6 py-4">
          {#if substitutesQuery.isPending}
            <p class="text-sm text-muted-foreground">Loading…</p>
          {:else if substitutesQuery.isError}
            <p class="text-sm text-destructive">Failed to load substitutes.</p>
          {:else if substitutesQuery.data.items.length === 0}
            <p class="text-sm text-muted-foreground py-6 text-center">No substitutes configured. Use the other tabs to add some.</p>
          {:else}
            <p class="text-xs text-muted-foreground mb-3">Substitutes are tried in order — the first with sufficient free stock is used.</p>
            <div class="border border-border rounded-md divide-y divide-border">
              {#each substitutesQuery.data.items as sub, i (sub.part_id)}
                {@const isFirst = i === 0}
                {@const isLast = i === substitutesQuery.data.items.length - 1}
                <div class="flex items-center gap-3 px-3 py-2.5">
                  <span class="text-xs text-muted-foreground tabular-nums w-4 shrink-0">{i + 1}</span>
                  <span class="font-mono text-sm flex-1">{sub.part_mpn ?? sub.part_id}</span>
                  <div class="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-7 w-7"
                      disabled={isFirst || reordering}
                      onclick={() => handleMove(sub.part_id, 'up')}
                      title="Move up"
                    >
                      <ArrowUp class="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-7 w-7"
                      disabled={isLast || reordering}
                      onclick={() => handleMove(sub.part_id, 'down')}
                      title="Move down"
                    >
                      <ArrowDown class="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-7 w-7 text-destructive hover:text-destructive"
                      disabled={removing[sub.part_id]}
                      onclick={() => handleRemove(sub.part_id)}
                      title="Remove"
                    >
                      <Trash2 class="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </TabsContent>

        <!-- Tab 2: Import from previous revision -->
        <TabsContent value="import" class="mt-0 flex-1 min-h-0 overflow-auto px-6 py-4">
          {#if previousRevisionsQuery.isPending}
            <p class="text-sm text-muted-foreground">Loading…</p>
          {:else if previousRevisionsQuery.isError}
            <p class="text-sm text-destructive">Failed to load previous revisions.</p>
          {:else if previousRevisionsQuery.data.items.length === 0}
            <p class="text-sm text-muted-foreground py-6 text-center">
              No previous revisions with substitutes found for the same BOM line item (matched by resolved part).
            </p>
          {:else}
            <p class="text-sm text-muted-foreground mb-4">
              Select a previous revision to copy its substitutes for this line item.
            </p>
            <div class="space-y-2 mb-5">
              {#each previousRevisionsQuery.data.items as rev (rev.id)}
                <label class="flex items-center gap-3 p-3 rounded-md border border-border cursor-pointer hover:bg-accent/50 transition-colors has-[:checked]:border-primary has-[:checked]:bg-primary/5">
                  <input
                    type="radio"
                    name="import-revision"
                    value={rev.id}
                    bind:group={selectedImportRevId}
                    class="accent-primary"
                  />
                  <div>
                    <span class="text-sm font-medium">{rev.label}</span>
                    <span class="text-xs text-muted-foreground ml-2">{new Date(rev.imported_at).toLocaleDateString()}</span>
                  </div>
                </label>
              {/each}
            </div>
            <Button
              size="sm"
              disabled={!selectedImportRevId || importing}
              onclick={handleImport}
            >
              <Download class="h-4 w-4 mr-1.5" />
              {importing ? 'Importing…' : 'Import Substitutes'}
            </Button>
          {/if}
        </TabsContent>

        <!-- Tab 3: Add from catalog -->
        <TabsContent value="catalog" class="mt-0 flex-1 min-h-0 overflow-auto px-6 pt-3 pb-6">
          <p class="text-xs text-muted-foreground mb-3">Click a part to add it as a substitute.</p>
          <PartsTable
            {teamId}
            onRowClick={handleAddPart}
            syncUrl={false}
          />
        </TabsContent>
      </Tabs>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

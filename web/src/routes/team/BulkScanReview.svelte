<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listBulkScanItems } from '$lib/api/bulkScan.js';
  import PartImportReview from '$lib/components/part-import/PartImportReview.svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { linkAction } from '$lib/router/index.svelte.js';

  let { teamId, sessionId }: { teamId: string; sessionId: string } = $props();

  const qc = useQueryClient();

  const itemsQuery = createQuery(() => ({
    queryKey: ['bulk-scan-items', teamId, sessionId],
    queryFn: () => listBulkScanItems(teamId, sessionId),
    refetchInterval: 5000,
  }));

  const items = $derived(itemsQuery.data?.items ?? []);

  function onItemActioned(_itemId: string, _action: 'reviewed' | 'dismissed') {
    qc.invalidateQueries({ queryKey: ['bulk-scan-items', teamId, sessionId] });
    qc.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Review">
    {#snippet actions()}
      <a
        href={`/teams/${teamId}/bulk-scan`}
        use:linkAction
        class="text-sm text-muted-foreground hover:text-foreground"
      >← All sessions</a>
    {/snippet}
  </Topbar>

  {#if itemsQuery.isPending}
    <div class="flex flex-1 items-center justify-center">
      <p class="text-sm text-muted-foreground">Loading…</p>
    </div>
  {:else if itemsQuery.isError}
    <div class="flex flex-1 items-center justify-center">
      <p class="text-sm text-destructive">Failed to load items.</p>
    </div>
  {:else}
    <PartImportReview {items} {teamId} {sessionId} {onItemActioned} />
  {/if}
</div>

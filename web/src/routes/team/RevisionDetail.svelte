<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getProject } from '$lib/api/projects.js';
  import type { BOMLineItem } from '$lib/types/projects.js';
  import { getRevision, updateBOMLineItem } from '$lib/api/revisions.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import BOMImportProgress from '$lib/components/bom/BOMImportProgress.svelte';
  import BOMLineItemRow from '$lib/components/bom/BOMLineItemRow.svelte';
  import BOMReviewDialog from '$lib/components/bom/BOMReviewDialog.svelte';
  import { Table, TableHeader, TableBody, TableHead, TableRow } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ClipboardCheck } from 'lucide-svelte';

  let {
    teamId,
    projectId,
    revId,
    importJobId = null,
  }: {
    teamId: string;
    projectId: string;
    revId: string;
    importJobId?: string | null;
  } = $props();

  const qc = useQueryClient();

  // Track whether the import job has been dismissed (completed)
  let jobDismissed = $state(false);
  let activeJobId = $derived(jobDismissed ? null : importJobId);

  type FilterTab = 'all' | 'needs_attention' | 'auto' | 'ignored';
  let activeFilter = $state<FilterTab>('all');

  // Review dialog state
  let reviewOpen = $state(false);
  let reviewItems = $state<BOMLineItem[]>([]);

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let revisionQuery = createQuery(() => ({
    queryKey: ['revision', teamId, projectId, revId],
    queryFn: () => getRevision(teamId, projectId, revId),
  }));

  let allItems = $derived(revisionQuery.data?.items ?? []);

  let isReady = $derived(
    allItems.length > 0 && allItems.every(i => i.resolved_part_id != null || i.ignored)
  );

  let needsAttentionCount = $derived(
    allItems.filter(i => !i.ignored && !i.resolved_part_id).length
  );

  let filteredItems = $derived((): BOMLineItem[] => {
    switch (activeFilter) {
      case 'needs_attention':
        return allItems.filter(i => !i.ignored && !i.resolved_part_id);
      case 'auto':
        return allItems.filter(i => i.resolution_state === 'auto_sku' || i.resolution_state === 'auto_mpn');
      case 'ignored':
        return allItems.filter(i => i.ignored);
      default:
        return allItems;
    }
  });

  function handleImportComplete() {
    jobDismissed = true;
    qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
    pushToast('success', 'BOM import complete');
  }

  async function handleResolve(itemId: string, partId: string | null) {
    try {
      await updateBOMLineItem(teamId, projectId, revId, itemId, { resolved_part_id: partId });
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
      pushToast('success', partId ? 'Part resolved' : 'Part cleared');
    } catch (err: any) {
      pushToast('error', 'Failed to update line item', err.message);
    }
  }

  async function handleIgnore(itemId: string, ignored: boolean) {
    try {
      await updateBOMLineItem(teamId, projectId, revId, itemId, { ignored });
      reviewItems = reviewItems.map(i => i.id === itemId ? { ...i, ignored } : i);
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
      pushToast('success', ignored ? 'Item ignored' : 'Item un-ignored');
    } catch (err: any) {
      pushToast('error', 'Failed to update line item', err.message);
    }
  }

  function startFullReview() {
    reviewItems = allItems.filter(i => !i.ignored && !i.resolved_part_id);
    reviewOpen = true;
  }

  function startReviewForItem(itemId: string) {
    const item = allItems.find(i => i.id === itemId);
    reviewItems = item ? [item] : [];
    reviewOpen = true;
  }

  function handleReviewClose() {
    reviewOpen = false;
  }

  let projectName = $derived(projectQuery.data?.name ?? 'Project');
  let revisionLabel = $derived(revisionQuery.data?.label ?? 'Revision');

  const filterTabs: { id: FilterTab; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'needs_attention', label: 'Needs Attention' },
    { id: 'auto', label: 'Auto-resolved' },
    { id: 'ignored', label: 'Ignored' },
  ];
</script>

<div class="flex flex-col h-full">
  <Topbar
    title={revisionLabel}
    breadcrumbs={[
      { label: 'Projects', href: `/teams/${teamId}/projects` },
      { label: projectName, href: `/teams/${teamId}/projects/${projectId}` },
      { label: revisionLabel },
    ]}
  >
    {#snippet actions()}
      <Button
        size="sm"
        onclick={() => navigate(`/teams/${teamId}/projects/${projectId}/revisions/${revId}/stock`)}
      >
        <ClipboardCheck class="h-4 w-4 mr-1" /> Check Stock
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <!-- Import progress banner -->
    {#if activeJobId}
      <BOMImportProgress
        {teamId}
        jobId={activeJobId}
        oncomplete={handleImportComplete}
      />
    {/if}

    <!-- Revision meta -->
    {#if revisionQuery.data}
      {@const rev = revisionQuery.data}
      <div class="flex items-center gap-3 text-sm text-muted-foreground flex-wrap">
        {#if rev.is_active}
          <Badge variant="success">Active BOM</Badge>
        {/if}
        {#if allItems.length > 0}
          {#if isReady}
            <Badge variant="success">Ready</Badge>
          {:else}
            <Badge variant="warning">Needs Attention{needsAttentionCount > 0 ? ` (${needsAttentionCount})` : ''}</Badge>
            {#if needsAttentionCount > 0}
              <Button size="sm" variant="outline" onclick={startFullReview}>
                Review ({needsAttentionCount})
              </Button>
            {/if}
          {/if}
        {/if}
        <span>Imported by {rev.imported_by_display_name}</span>
        {#if rev.notes}
          <span>· {rev.notes}</span>
        {/if}
      </div>
    {/if}

    <!-- BOM line items table -->
    {#if revisionQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if revisionQuery.isError}
      <div class="text-sm text-destructive">Error: {revisionQuery.error.message}</div>
    {:else if revisionQuery.data}
      {#if allItems.length === 0}
        <EmptyState
          title="No line items"
          description="This revision has no BOM line items."
        />
      {:else}
        <!-- Filter tabs -->
        <div class="flex gap-1 border-b border-border pb-2">
          {#each filterTabs as tab (tab.id)}
            <button
              type="button"
              class="px-3 py-1 text-sm rounded-md transition-colors
                     {activeFilter === tab.id
                       ? 'bg-primary text-primary-foreground'
                       : 'text-muted-foreground hover:text-foreground hover:bg-muted'}"
              onclick={() => { activeFilter = tab.id; }}
            >
              {tab.label}
            </button>
          {/each}
        </div>

        {@const items = filteredItems().sort((a, b) => b.quantity - a.quantity)}
        {#if items.length === 0}
          <div class="text-sm text-muted-foreground py-4 text-center">No items in this filter.</div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Manufacturer</TableHead>
                <TableHead>MPN</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Reference Designators</TableHead>
                <TableHead>Distributor Name</TableHead>
                <TableHead>SKU</TableHead>
                <TableHead class="text-center">Qty</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each items as item (item.id)}
                <BOMLineItemRow
                  {item}
                  onstatusclick={(id) => startReviewForItem(id)}
                />
              {/each}
            </TableBody>
          </Table>
        {/if}
      {/if}
    {/if}
  </div>
</div>

<BOMReviewDialog
  items={reviewItems}
  {teamId}
  {projectId}
  revisionId={revId}
  bind:open={reviewOpen}
  onclose={handleReviewClose}
  onresolve={handleResolve}
  onignore={handleIgnore}
/>

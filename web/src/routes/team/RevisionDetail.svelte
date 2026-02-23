<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getProject } from '$lib/api/projects.js';
  import { getRevision, updateBOMLineItem, listAllSubstitutes, type SubstituteItem } from '$lib/api/revisions.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import BOMImportProgress from '$lib/components/bom/BOMImportProgress.svelte';
  import BOMLineItemRow from '$lib/components/bom/BOMLineItemRow.svelte';
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

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let revisionQuery = createQuery(() => ({
    queryKey: ['revision', teamId, projectId, revId],
    queryFn: () => getRevision(teamId, projectId, revId),
  }));

  let substitutesQuery = createQuery(() => ({
    queryKey: ['substitutes', teamId, projectId, revId],
    queryFn: () => listAllSubstitutes(teamId, projectId, revId),
  }));

  // Map from bom_line_item_id → substitute list for O(1) lookup per row.
  let substituteMap = $derived(() => {
    const map = new Map<string, SubstituteItem[]>();
    for (const sub of substitutesQuery.data?.items ?? []) {
      const existing = map.get(sub.bom_line_item_id) ?? [];
      existing.push(sub);
      map.set(sub.bom_line_item_id, existing);
    }
    return map;
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

  function handleSubstituteChange() {
    qc.invalidateQueries({ queryKey: ['substitutes', teamId, projectId, revId] });
  }

  let projectName = $derived(projectQuery.data?.name ?? 'Project');
  let revisionLabel = $derived(revisionQuery.data?.label ?? 'Revision');
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
      <div class="flex items-center gap-3 text-sm text-muted-foreground">
        {#if rev.is_active}
          <Badge variant="success">Active BOM</Badge>
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
      {@const items = revisionQuery.data.items}
      {#if items.length === 0}
        <EmptyState
          title="No line items"
          description="This revision has no BOM line items."
        />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Ref Des</TableHead>
              <TableHead class="text-center">Qty</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Resolve Part</TableHead>
              <TableHead>Substitutes</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each items as item (item.id)}
              <BOMLineItemRow
                {item}
                {teamId}
                {projectId}
                revisionId={revId}
                substitutes={substituteMap().get(item.id) ?? []}
                onresolve={handleResolve}
                onsubstitutechange={handleSubstituteChange}
              />
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>

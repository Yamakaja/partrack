<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getProject, setActiveBOM } from '$lib/api/projects.js';
  import { listRevisions } from '$lib/api/revisions.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import BOMImportProgress from '$lib/components/bom/BOMImportProgress.svelte';
  import BOMImportDialog from '$lib/components/bom/BOMImportDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Upload } from 'lucide-svelte';

  let { teamId, projectId }: { teamId: string; projectId: string } = $props();

  const qc = useQueryClient();

  // Running job state
  let activeJobId = $state<string | null>(null);
  let activeRevId = $state<string | null>(null);

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let revisionsQuery = createQuery(() => ({
    queryKey: ['revisions', teamId, projectId],
    queryFn: () => listRevisions(teamId, projectId),
  }));

  function handleImportStarted(jobId: string, revisionId: string) {
    activeJobId = jobId;
    activeRevId = revisionId;
    qc.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
  }

  function handleImportComplete() {
    qc.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
    pushToast('success', 'BOM import complete');
    if (activeRevId) {
      navigate(`/teams/${teamId}/projects/${projectId}/revisions/${activeRevId}`);
    }
    activeJobId = null;
    activeRevId = null;
  }

  async function handleSetActive(e: MouseEvent, revId: string) {
    e.stopPropagation();
    try {
      await setActiveBOM(teamId, projectId, revId);
      qc.invalidateQueries({ queryKey: ['project', teamId, projectId] });
      qc.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
      pushToast('success', 'Active BOM updated');
    } catch (err: any) {
      pushToast('error', 'Failed to set active BOM', err.message);
    }
  }

  function handleViewRevision(e: MouseEvent, revId: string) {
    e.stopPropagation();
    navigate(`/teams/${teamId}/projects/${projectId}/revisions/${revId}`);
  }

  function formatDate(iso: string) {
    return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  }

  let projectName = $derived(projectQuery.data?.name ?? 'Project');
</script>

<div class="flex flex-col h-full">
  <Topbar
    title={projectName}
    breadcrumbs={[
      { label: 'Projects', href: `/teams/${teamId}/projects` },
      { label: projectName },
    ]}
  >
    {#snippet actions()}
      <BOMImportDialog
        {teamId}
        {projectId}
        initialConfig={projectQuery.data?.bom_import_config ?? null}
        oncomplete={handleImportStarted}
      >
        {#snippet trigger(open)}
          <Button size="sm" onclick={open}>
            <Upload class="h-4 w-4 mr-1" /> Import BOM
          </Button>
        {/snippet}
      </BOMImportDialog>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">
    <!-- Project info -->
    {#if projectQuery.data}
      {@const proj = projectQuery.data}
      {#if proj.description}
        <p class="text-sm text-muted-foreground">{proj.description}</p>
      {/if}
    {/if}

    <!-- Import progress banner -->
    {#if activeJobId}
      <BOMImportProgress
        {teamId}
        jobId={activeJobId}
        oncomplete={handleImportComplete}
      />
    {/if}

    <!-- Revisions table -->
    <div>
      <h2 class="text-sm font-semibold mb-3">BOM Revisions</h2>
      {#if revisionsQuery.isPending}
        <div class="text-sm text-muted-foreground">Loading…</div>
      {:else if revisionsQuery.isError}
        <div class="text-sm text-destructive">Error: {revisionsQuery.error.message}</div>
      {:else if revisionsQuery.data}
        {@const revisions = revisionsQuery.data.items}
        {#if revisions.length === 0}
          <EmptyState
            title="No BOM revisions"
            description="Import a BOM file to create the first revision."
          >
            {#snippet action()}
              <BOMImportDialog
                {teamId}
                {projectId}
                initialConfig={projectQuery.data?.bom_import_config ?? null}
                oncomplete={handleImportStarted}
              >
                {#snippet trigger(open)}
                  <Button size="sm" onclick={open}>
                    <Upload class="h-4 w-4 mr-1" /> Import BOM
                  </Button>
                {/snippet}
              </BOMImportDialog>
            {/snippet}
          </EmptyState>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Label</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Items</TableHead>
                <TableHead>Resolved</TableHead>
                <TableHead>Imported</TableHead>
                <TableHead>By</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each revisions as rev (rev.id)}
                <TableRow
                  class="cursor-pointer"
                  onclick={() => navigate(`/teams/${teamId}/projects/${projectId}/revisions/${rev.id}`)}
                >
                  <TableCell class="font-medium">{rev.label}</TableCell>
                  <TableCell>
                    {#if rev.is_active}
                      <Badge variant="success">Active</Badge>
                    {:else}
                      <Badge variant="outline">Archived</Badge>
                    {/if}
                  </TableCell>
                  <TableCell class="tabular-nums text-muted-foreground">{rev.item_count}</TableCell>
                  <TableCell class="tabular-nums text-muted-foreground">
                    {rev.resolved_count}/{rev.item_count}
                  </TableCell>
                  <TableCell class="text-muted-foreground text-sm">
                    {formatDate(rev.imported_at)}
                  </TableCell>
                  <TableCell class="text-muted-foreground text-sm">
                    {rev.imported_by_display_name}
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-1">
                      {#if !rev.is_active}
                        <Button
                          variant="ghost"
                          size="sm"
                          onclick={(e: MouseEvent) => handleSetActive(e, rev.id)}
                        >
                          Set Active
                        </Button>
                      {/if}
                      <Button
                        variant="ghost"
                        size="sm"
                        onclick={(e: MouseEvent) => handleViewRevision(e, rev.id)}
                      >
                        View
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
</div>


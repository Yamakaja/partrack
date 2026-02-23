<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getProject, setActiveBOM } from '$lib/api/projects.js';
  import { listRevisions, importRevision } from '$lib/api/revisions.js';
  import type { ImportRevisionBody } from '$lib/api/revisions.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import BOMImportProgress from '$lib/components/bom/BOMImportProgress.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Upload } from 'lucide-svelte';

  let { teamId, projectId }: { teamId: string; projectId: string } = $props();

  const qc = useQueryClient();

  // Import dialog state
  let importOpen = $state(false);
  let importLabel = $state('');
  let importRefCol = $state('Reference');
  let importMpnCol = $state('MPN');
  let importQtyCol = $state('Quantity');
  let importDescCol = $state('Description');
  let importing = $state(false);

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

  function openImport() {
    importLabel = '';
    importRefCol = 'Reference';
    importMpnCol = 'MPN';
    importQtyCol = 'Quantity';
    importDescCol = 'Description';
    importOpen = true;
  }

  async function handleImport(e: Event) {
    e.preventDefault();
    if (!importLabel.trim()) return;
    importing = true;
    try {
      const body: ImportRevisionBody = {
        label: importLabel.trim(),
        bom_import_config: {
          ref_col: importRefCol.trim(),
          mpn_col: importMpnCol.trim(),
          qty_col: importQtyCol.trim(),
          description_col: importDescCol.trim(),
        },
      };
      const result = await importRevision(teamId, projectId, body);
      importOpen = false;
      activeJobId = result.job_id;
      activeRevId = result.bom_revision_id;
      qc.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
      pushToast('info', 'BOM import started');
    } catch (err: any) {
      pushToast('error', 'Import failed', err.message);
    } finally {
      importing = false;
    }
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
      <Button size="sm" onclick={openImport}>
        <Upload class="h-4 w-4 mr-1" /> Import BOM
      </Button>
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
              <Button size="sm" onclick={openImport}>
                <Upload class="h-4 w-4 mr-1" /> Import BOM
              </Button>
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

<!-- Import BOM Dialog -->
<Dialog.Root bind:open={importOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Import BOM</Dialog.Title>
      <Dialog.Description class="text-sm text-muted-foreground mb-4">
        Specify the revision label and column names from your CSV file.
      </Dialog.Description>
      <form onsubmit={handleImport} class="space-y-4">
        <div class="space-y-1">
          <Label for="rev-label">Revision Name <span class="text-destructive">*</span></Label>
          <Input
            id="rev-label"
            bind:value={importLabel}
            placeholder="e.g. v1.0, Rev A"
            required
          />
        </div>
        <fieldset class="space-y-3">
          <legend class="text-sm font-medium">Column Mapping</legend>
          <p class="text-xs text-muted-foreground">
            Enter the exact column header names from your CSV file.
          </p>
          <div class="grid grid-cols-2 gap-3">
            <div class="space-y-1">
              <Label for="ref-col">Reference Designators</Label>
              <Input id="ref-col" bind:value={importRefCol} placeholder="Reference" />
            </div>
            <div class="space-y-1">
              <Label for="mpn-col">MPN</Label>
              <Input id="mpn-col" bind:value={importMpnCol} placeholder="MPN" />
            </div>
            <div class="space-y-1">
              <Label for="qty-col">Quantity</Label>
              <Input id="qty-col" bind:value={importQtyCol} placeholder="Quantity" />
            </div>
            <div class="space-y-1">
              <Label for="desc-col">Description</Label>
              <Input id="desc-col" bind:value={importDescCol} placeholder="Description" />
            </div>
          </div>
        </fieldset>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => (importOpen = false)}>Cancel</Button>
          <Button type="submit" disabled={importing || !importLabel.trim()}>
            {importing ? 'Importing…' : 'Start Import'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

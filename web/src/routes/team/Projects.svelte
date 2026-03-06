<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listProjects, createProject } from '$lib/api/projects.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import StockCheckDialog from '$lib/components/stock/StockCheckDialog.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus, ClipboardCheck } from 'lucide-svelte';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import { projectStatusVariant } from '$lib/types/projects';

  let { teamId }: { teamId: string } = $props();

  type ProjectRow = NonNullable<typeof query.data>['items'][number];

  const qc = useQueryClient();

  let createOpen = $state(false);
  let newName = $state('');
  let newDescription = $state('');
  let creating = $state(false);

  let query = createQuery(() => ({
    queryKey: ['projects', teamId],
    queryFn: () => listProjects(teamId),
  }));

  const columns: ColumnSpec<ProjectRow>[] = [
    { key: 'name', header: 'Name', type: 'text', filterable: true },
    { key: 'description', header: 'Description', type: 'text' },
    {
      key: 'status',
      header: 'Status',
      type: 'enum',
      filterable: true,
      filterValues: ['active', 'completed', 'archived'],
      filterLabels: { active: 'Active', completed: 'Completed', archived: 'Archived' },
    },
    { key: 'active_bom_revision_label', header: 'Active BOM', type: 'text' },
    { key: 'created_at', header: 'Created', type: 'date', filterable: true },
  ];

  const source = new LocalDataSource<ProjectRow>(
    () => query.data?.items ?? [],
    columns,
  );

  function openCreate() {
    newName = '';
    newDescription = '';
    createOpen = true;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    if (!newName.trim()) return;
    creating = true;
    try {
      const project = await createProject(teamId, newName.trim(), newDescription.trim() || undefined);
      createOpen = false;
      qc.invalidateQueries({ queryKey: ['projects', teamId] });
      pushToast('success', 'Project created');
      navigate(`/teams/${teamId}/projects/${project.id}`);
    } catch (err: any) {
      pushToast('error', 'Failed to create project', err.message);
    } finally {
      creating = false;
    }
  }


</script>

<div class="flex flex-col h-full">
  <Topbar title="Projects">
    {#snippet actions()}
      <Button size="sm" onclick={openCreate}>
        <Plus class="h-4 w-4 mr-1" /> New Project
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if query.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if query.isError}
      <div class="text-sm text-destructive">Error: {query.error.message}</div>
    {:else if query.data?.items.length === 0}
      <EmptyState
        title="No projects yet"
        description="Create your first project to start tracking BOMs."
      >
        {#snippet action()}
          <Button size="sm" onclick={openCreate}>
            <Plus class="h-4 w-4 mr-1" /> New Project
          </Button>
        {/snippet}
      </EmptyState>
    {:else}
      {#snippet renderDescription(value: unknown, _row: ProjectRow)}
        <span class="text-muted-foreground">{value ?? '—'}</span>
      {/snippet}

      {#snippet renderStatus(value: unknown, _row: ProjectRow)}
        <Badge variant={projectStatusVariant(value as ProjectRow['status'])} class="capitalize">{value as string}</Badge>
      {/snippet}

      {#snippet renderActiveBom(value: unknown, _row: ProjectRow)}
        <span class="text-muted-foreground text-sm">{value ?? '—'}</span>
      {/snippet}

      {#snippet renderCreatedAt(value: unknown, _row: ProjectRow)}
        <DateDisplay value={String(value)} />
      {/snippet}

      {#snippet actions(project: ProjectRow)}
        {#if project.active_bom_revision_id}
          <StockCheckDialog
            teamId={teamId}
            projectId={project.id}
            revId={project.active_bom_revision_id}
          >
            {#snippet trigger(open)}
              <Button size="sm" onclick={(e: MouseEvent) => { e.stopPropagation(); open(e); }}>
                <ClipboardCheck class="h-4 w-4 mr-1" /> Check Stock
              </Button>
            {/snippet}
          </StockCheckDialog>
        {/if}
      {/snippet}

      <DataTable
        {source}
        getRowKey={(p) => p.id}
        onRowClick={(p) => navigate(`/teams/${teamId}/projects/${p.id}`)}
        renderers={{
          description: renderDescription,
          status: renderStatus,
          active_bom_revision_label: renderActiveBom,
          created_at: renderCreatedAt,
        }}
        {actions}
        emptyMessage="No projects yet"
        exportFilename="projects.csv"
      />
    {/if}
  </div>
</div>

<!-- New Project Dialog -->
<Dialog.Root bind:open={createOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">New Project</Dialog.Title>
      <form onsubmit={handleCreate} class="space-y-4">
        <div class="space-y-1">
          <Label for="proj-name">Name <span class="text-destructive">*</span></Label>
          <Input
            id="proj-name"
            bind:value={newName}
            placeholder="e.g. Motor Controller v2"
            required
          />
        </div>
        <div class="space-y-1">
          <Label for="proj-desc">Description</Label>
          <Input
            id="proj-desc"
            bind:value={newDescription}
            placeholder="Optional description"
          />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => (createOpen = false)}>Cancel</Button>
          <Button type="submit" disabled={creating || !newName.trim()}>
            {creating ? 'Creating…' : 'Create Project'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

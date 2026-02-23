<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listProjects, createProject } from '$lib/api/projects.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let createOpen = $state(false);
  let newName = $state('');
  let newDescription = $state('');
  let creating = $state(false);

  let query = createQuery(() => ({
    queryKey: ['projects', teamId],
    queryFn: () => listProjects(teamId),
  }));

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

  function formatDate(iso: string) {
    return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
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
    {:else if query.data}
      {@const items = query.data.items}
      {#if items.length === 0}
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
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Active BOM</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each items as project (project.id)}
              <TableRow
                class="cursor-pointer"
                onclick={() => navigate(`/teams/${teamId}/projects/${project.id}`)}
              >
                <TableCell class="font-medium">{project.name}</TableCell>
                <TableCell class="max-w-xs truncate text-muted-foreground">
                  {project.description ?? '—'}
                </TableCell>
                <TableCell class="text-muted-foreground text-sm">
                  {project.active_bom_revision_label ?? '—'}
                </TableCell>
                <TableCell class="text-muted-foreground text-sm">
                  {formatDate(project.created_at)}
                </TableCell>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="sm"
                    onclick={(e: MouseEvent) => {
                      e.stopPropagation();
                      navigate(`/teams/${teamId}/projects/${project.id}`);
                    }}
                  >
                    View
                  </Button>
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
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

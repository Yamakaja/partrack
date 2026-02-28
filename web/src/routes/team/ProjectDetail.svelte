<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getProject, setActiveBOM, updateProject } from '$lib/api/projects.js';
  import { listRevisions } from '$lib/api/revisions.js';
  import { listMembers } from '$lib/api/teams.js';
  import { listDistributorConfigs } from '$lib/api/distributors.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import BOMImportDialog from '$lib/components/bom/BOMImportDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Upload, Pencil } from 'lucide-svelte';

  let { teamId, projectId }: { teamId: string; projectId: string } = $props();

  const qc = useQueryClient();

  // Edit project dialog
  let editOpen = $state(false);
  let editName = $state('');
  let editDescription = $state('');
  let editStatus = $state('active');
  let editSourceUrl = $state('');
  let editProjectManagerId = $state('');
  let editPreferredDistributorId = $state('');
  let editSaving = $state(false);

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let revisionsQuery = createQuery(() => ({
    queryKey: ['revisions', teamId, projectId],
    queryFn: () => listRevisions(teamId, projectId),
  }));

  let membersQuery = createQuery(() => ({
    queryKey: ['members', teamId],
    queryFn: () => listMembers(teamId),
  }));

  let distributorConfigsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
  }));

  function handleImportStarted() {
    qc.invalidateQueries({ queryKey: ['revisions', teamId, projectId] });
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

  function openEdit() {
    const proj = projectQuery.data;
    if (!proj) return;
    editName = proj.name;
    editDescription = proj.description ?? '';
    editStatus = proj.status;
    editSourceUrl = proj.source_url ?? '';
    editProjectManagerId = proj.project_manager_id ?? '';
    editPreferredDistributorId = proj.preferred_distributor_id ?? '';
    editOpen = true;
  }

  async function handleEditSave(e: Event) {
    e.preventDefault();
    editSaving = true;
    try {
      await updateProject(teamId, projectId, {
        name: editName.trim() || undefined,
        description: editDescription.trim() || undefined,
        status: editStatus as 'active' | 'archived' | 'completed',
        source_url: editSourceUrl.trim() || null,
        project_manager_id: editProjectManagerId || null,
        preferred_distributor_id: editPreferredDistributorId || null,
      });
      editOpen = false;
      qc.invalidateQueries({ queryKey: ['project', teamId, projectId] });
      qc.invalidateQueries({ queryKey: ['projects', teamId] });
      pushToast('success', 'Project updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update project', err.message);
    } finally {
      editSaving = false;
    }
  }

  function formatDate(iso: string) {
    return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  }

  const STATUS_LABELS: Record<string, string> = {
    active: 'In Development',
    completed: 'In Production',
    archived: 'Archived',
  };

  let projectName = $derived(projectQuery.data?.name ?? 'Project');
  let enabledDistributors = $derived(
    (distributorConfigsQuery.data?.items ?? []).filter((d) => d.enabled),
  );
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
    <!-- Project metadata card -->
    {#if projectQuery.data}
      {@const proj = projectQuery.data}
      <div class="rounded-lg border border-border bg-card p-4 space-y-3">
        <div class="flex items-start justify-between">
          <div>
            <div class="flex items-center gap-2 flex-wrap">
              <h2 class="text-xl font-bold">{proj.name}</h2>
              <Badge variant="outline">{STATUS_LABELS[proj.status] ?? proj.status}</Badge>
            </div>
            {#if proj.description}
              <p class="text-sm text-muted-foreground mt-1">{proj.description}</p>
            {/if}
          </div>
          <Button variant="outline" size="sm" onclick={openEdit}>
            <Pencil class="h-4 w-4 mr-1" /> Edit
          </Button>
        </div>

        <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm">
          <div>
            <span class="text-muted-foreground">Created by:</span>
            <span class="font-medium ml-1">{proj.created_by_display_name}</span>
          </div>
          <div>
            <span class="text-muted-foreground">Created:</span>
            <span class="font-medium ml-1">{formatDate(proj.created_at)}</span>
          </div>
          {#if proj.project_manager_display_name}
            <div>
              <span class="text-muted-foreground">Project manager:</span>
              <span class="font-medium ml-1">{proj.project_manager_display_name}</span>
            </div>
          {/if}
          {#if proj.source_url}
            <div>
              <span class="text-muted-foreground">Source:</span>
              <a
                href={proj.source_url}
                target="_blank"
                rel="noopener noreferrer"
                class="font-medium ml-1 text-primary hover:underline"
              >Link</a>
            </div>
          {/if}
        </div>

        {#if proj.active_revision_summary}
          {@const s = proj.active_revision_summary}
          <div class="grid grid-cols-3 gap-4 pt-3 border-t border-border">
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">BOM Items</p>
              <p class="text-xl font-bold tabular-nums">{s.item_count}</p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">Resolved</p>
              <p class="text-xl font-bold tabular-nums text-green-600">{s.resolved_count}</p>
            </div>
            <div>
              <p class="text-xs text-muted-foreground mb-0.5">Unresolved</p>
              <p class="text-xl font-bold tabular-nums {s.unresolved_count > 0 ? 'text-destructive' : ''}">{s.unresolved_count}</p>
            </div>
          </div>
        {/if}
      </div>
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

<!-- Edit Project Dialog -->
<Dialog.Root bind:open={editOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Edit Project</Dialog.Title>
      <form onsubmit={handleEditSave} class="space-y-4">
        <div class="space-y-1">
          <Label for="edit-name">Name <span class="text-destructive">*</span></Label>
          <Input id="edit-name" bind:value={editName} required />
        </div>
        <div class="space-y-1">
          <Label for="edit-desc">Description</Label>
          <Input id="edit-desc" bind:value={editDescription} placeholder="Optional description" />
        </div>
        <div class="space-y-1">
          <Label for="edit-status">Status</Label>
          <select
            id="edit-status"
            bind:value={editStatus}
            class="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <option value="active">In Development</option>
            <option value="completed">In Production</option>
            <option value="archived">Archived</option>
          </select>
        </div>
        <div class="space-y-1">
          <Label for="edit-source-url">Source URL</Label>
          <Input id="edit-source-url" bind:value={editSourceUrl} placeholder="https://…" />
        </div>
        <div class="space-y-1">
          <Label for="edit-pm">Project Manager</Label>
          <select
            id="edit-pm"
            bind:value={editProjectManagerId}
            class="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <option value="">— None —</option>
            {#each membersQuery.data?.items ?? [] as member (member.user_id)}
              <option value={member.user_id}>{member.display_name}</option>
            {/each}
          </select>
        </div>
        <div class="space-y-1">
          <Label for="edit-pref-dist">Preferred Distributor</Label>
          <select
            id="edit-pref-dist"
            bind:value={editPreferredDistributorId}
            class="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <option value="">— Team default —</option>
            {#each enabledDistributors as dist (dist.distributor_id)}
              <option value={dist.distributor_id}>{dist.distributor_name}</option>
            {/each}
          </select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => (editOpen = false)}>Cancel</Button>
          <Button type="submit" disabled={editSaving || !editName.trim()}>
            {editSaving ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

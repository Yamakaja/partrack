<script lang="ts">
  import { createQuery, createMutation, useQueryClient } from '@tanstack/svelte-query';
  import { getProject } from '$lib/api/projects.js';
  import {
    listInterpretationMemory,
    deleteInterpretationMemoryEntry,
    type InterpretationMemoryEntry,
  } from '$lib/api/interpretation-memory.js';
  import { paramRaw, PACKAGE_SLUG, type Interpretation } from '$lib/api/revisions.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { formatRelative } from '$lib/utils/date.js';
  import { Trash2, Brain } from 'lucide-svelte';

  let { teamId, projectId }: { teamId: string; projectId: string } = $props();

  const qc = useQueryClient();

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let memoryQuery = createQuery(() => ({
    queryKey: ['interpretation-memory', teamId, projectId],
    queryFn: () => listInterpretationMemory(teamId, projectId),
  }));

  let entries = $derived(memoryQuery.data?.items ?? []);

  const deleteMutation = createMutation(() => ({
    mutationFn: (id: string) => deleteInterpretationMemoryEntry(teamId, projectId, id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['interpretation-memory', teamId, projectId] });
      pushToast('success', 'Interpretation forgotten', 'Future imports will re-parse that line.');
    },
    onError: (err: any) => pushToast('error', 'Failed to delete', err?.message ?? String(err)),
  }));

  let confirmOpen = $state(false);
  let pending = $state<InterpretationMemoryEntry | null>(null);

  function askDelete(entry: InterpretationMemoryEntry) {
    pending = entry;
    confirmOpen = true;
  }

  function confirmDelete() {
    if (pending) deleteMutation.mutate(pending.id);
    pending = null;
  }

  // The non-package parameters, rendered as "slug raw" chips.
  function valueParams(it: Interpretation) {
    return (it.params ?? []).filter((p) => p.slug !== PACKAGE_SLUG && p.raw);
  }

  const projectName = $derived(projectQuery.data?.name ?? 'Project');
</script>

<div class="flex flex-col h-full">
  <Topbar
    title="Interpretation Memory"
    breadcrumbs={[
      { label: 'Projects', href: `/teams/${teamId}/projects` },
      { label: projectName, href: `/teams/${teamId}/projects/${projectId}` },
    ]}
  />

  <div class="flex-1 overflow-auto p-6 space-y-6">
    <p class="text-sm text-muted-foreground max-w-3xl">
      When you correct how a BOM line is read in the import review gate, that reading is
      remembered for this project and reused on every later import — so recurring passives
      don't need re-reviewing. Delete an entry to make future imports parse that line fresh.
    </p>

    {#if memoryQuery.isLoading}
      <p class="text-sm text-muted-foreground">Loading…</p>
    {:else if memoryQuery.isError}
      <p class="text-sm text-destructive">Failed to load interpretation memory.</p>
    {:else if entries.length === 0}
      <div class="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed py-16 text-center">
        <Brain class="h-8 w-8 text-muted-foreground" />
        <p class="text-sm font-medium">Nothing remembered yet</p>
        <p class="text-sm text-muted-foreground max-w-md">
          Corrections you make in the BOM import review gate for this project will appear here.
        </p>
      </div>
    {:else}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead class="w-16">Class</TableHead>
            <TableHead>Value</TableHead>
            <TableHead>Footprint</TableHead>
            <TableHead>Reading</TableHead>
            <TableHead>Remembered by</TableHead>
            <TableHead>Updated</TableHead>
            <TableHead class="w-12"></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each entries as entry (entry.id)}
            {@const it = entry.interpretation}
            {@const pkg = paramRaw(it.params, PACKAGE_SLUG)}
            <TableRow>
              <TableCell><Badge variant="outline">{entry.class || '—'}</Badge></TableCell>
              <TableCell class="font-mono text-xs">{entry.value_key || '—'}</TableCell>
              <TableCell class="font-mono text-xs">{entry.footprint_key || '—'}</TableCell>
              <TableCell>
                <div class="flex flex-wrap items-center gap-1">
                  {#if it.category}<Badge variant="secondary">{it.category}</Badge>{/if}
                  {#if pkg}<Badge variant="outline">{pkg}</Badge>{/if}
                  {#each valueParams(it) as p}
                    <span class="text-xs text-muted-foreground">{p.slug} <span class="font-medium text-foreground">{p.raw}</span></span>
                  {/each}
                  {#if it.route && it.route !== 'parametric'}<Badge variant="outline">{it.route}</Badge>{/if}
                  {#if (it.confidence ?? 1) < 0.7}<Badge variant="destructive">low confidence</Badge>{/if}
                </div>
              </TableCell>
              <TableCell class="text-sm text-muted-foreground">{entry.created_by_name ?? '—'}</TableCell>
              <TableCell class="text-sm text-muted-foreground">{formatRelative(entry.updated_at)}</TableCell>
              <TableCell>
                <Button variant="ghost" size="icon" title="Forget this interpretation" onclick={() => askDelete(entry)}>
                  <Trash2 class="h-4 w-4 text-destructive" />
                </Button>
              </TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
    {/if}
  </div>
</div>

<ConfirmDialog
  bind:open={confirmOpen}
  title="Forget this interpretation?"
  description="Future imports of this project will re-parse this line from scratch instead of reusing the remembered reading. This does not change any already-imported revision."
  confirmLabel="Forget"
  destructive
  onconfirm={confirmDelete}
/>

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listBuilds, previewBuild, createBuild, type BuildPreviewItem } from '$lib/api/builds.js';
  import { listProjects } from '$lib/api/projects.js';
  import { listRevisions } from '$lib/api/revisions.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import Pagination from '$lib/components/common/Pagination.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus } from 'lucide-svelte';
  import type { ProjectSummary, Revision } from '$lib/types/projects.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let limit = $state(50);
  let offset = $state(0);

  let buildsQuery = createQuery(() => ({
    queryKey: ['builds', teamId, limit, offset],
    queryFn: () => listBuilds(teamId, { limit, offset }),
  }));

  // Dialog state
  let dialogOpen = $state(false);
  let step = $state(1); // 1=pick project+revision, 2=qty, 3=preview, 4=confirm
  let selectedProjectId = $state('');
  let selectedRevisionId = $state('');
  let buildQty = $state('1');
  let buildNotes = $state('');
  let previewItems = $state<BuildPreviewItem[]>([]);
  let previewing = $state(false);
  let submitting = $state(false);

  let projectsQuery = createQuery(() => ({
    queryKey: ['projects', teamId],
    queryFn: () => listProjects(teamId),
    enabled: dialogOpen,
  }));

  let revisionsQuery = createQuery(() => ({
    queryKey: ['revisions', teamId, selectedProjectId],
    queryFn: () => listRevisions(teamId, selectedProjectId),
    enabled: dialogOpen && !!selectedProjectId,
  }));

  function openDialog() {
    step = 1;
    selectedProjectId = '';
    selectedRevisionId = '';
    buildQty = '1';
    buildNotes = '';
    previewItems = [];
    dialogOpen = true;
  }

  async function handlePreview() {
    const qty = parseInt(buildQty, 10);
    if (!selectedProjectId || !selectedRevisionId || isNaN(qty) || qty < 1) return;
    previewing = true;
    try {
      const result = await previewBuild(teamId, {
        project_id: selectedProjectId,
        bom_revision_id: selectedRevisionId,
        quantity: qty,
      });
      previewItems = result.items;
      step = 3;
    } catch (err: any) {
      pushToast('error', 'Failed to preview build', err.message);
    } finally {
      previewing = false;
    }
  }

  async function handleConfirmBuild() {
    const qty = parseInt(buildQty, 10);
    if (!selectedProjectId || !selectedRevisionId || isNaN(qty) || qty < 1) return;
    submitting = true;
    try {
      await createBuild(teamId, {
        project_id: selectedProjectId,
        bom_revision_id: selectedRevisionId,
        quantity: qty,
        notes: buildNotes.trim() || undefined,
      });
      dialogOpen = false;
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      pushToast('success', 'Build recorded');
    } catch (err: any) {
      pushToast('error', 'Failed to record build', err.message);
    } finally {
      submitting = false;
    }
  }

  let step1Valid = $derived(!!selectedProjectId && !!selectedRevisionId);
  let step2Valid = $derived(() => {
    const q = parseInt(buildQty, 10);
    return !isNaN(q) && q >= 1;
  });
</script>

<div class="flex flex-col h-full">
  <Topbar title="Builds">
    {#snippet actions()}
      <Button size="sm" onclick={openDialog}>
        <Plus class="h-4 w-4 mr-1" /> Record Build
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    {#if buildsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading builds…</div>
    {:else if buildsQuery.isError}
      <div class="text-sm text-destructive">Error: {buildsQuery.error.message}</div>
    {:else if buildsQuery.data}
      {@const data = buildsQuery.data}
      {#if data.items.length === 0}
        <EmptyState
          title="No builds recorded"
          description="Record a build to track parts consumed during PCB assembly."
        >
          {#snippet action()}
            <Button size="sm" onclick={openDialog}>
              <Plus class="h-4 w-4 mr-1" /> Record Build
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Project</TableHead>
              <TableHead>Revision</TableHead>
              <TableHead>Qty Built</TableHead>
              <TableHead>Built At</TableHead>
              <TableHead>By</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each data.items as build (build.id)}
              <TableRow
                class="cursor-pointer"
                onclick={() => navigate(`/teams/${teamId}/builds/${build.id}`)}
              >
                <TableCell class="font-medium">{build.project_name}</TableCell>
                <TableCell class="text-muted-foreground text-sm">{build.bom_revision_label}</TableCell>
                <TableCell>{build.quantity_built}</TableCell>
                <TableCell>
                  <DateDisplay value={build.built_at} />
                </TableCell>
                <TableCell class="text-muted-foreground text-sm">{build.built_by_display_name}</TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
        <Pagination
          total={data.total}
          {limit}
          {offset}
          onchange={(o) => (offset = o)}
        />
      {/if}
    {/if}
  </div>
</div>

<!-- Record Build Dialog -->
<Dialog.Root bind:open={dialogOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Record Build</Dialog.Title>
      <p class="text-sm text-muted-foreground mb-4">Step {step} of 3</p>

      {#if step === 1}
        <div class="space-y-3">
          <div class="space-y-1">
            <Label for="build-project">Project <span class="text-destructive">*</span></Label>
            <Select
              id="build-project"
              bind:value={selectedProjectId}
            >
              <option value="">Select project…</option>
              {#each (projectsQuery.data?.items ?? []) as project (project.id)}
                <option value={project.id}>{project.name}</option>
              {/each}
            </Select>
          </div>
          <div class="space-y-1">
            <Label for="build-revision">BOM Revision <span class="text-destructive">*</span></Label>
            <Select
              id="build-revision"
              bind:value={selectedRevisionId}
              disabled={!selectedProjectId}
            >
              <option value="">Select revision…</option>
              {#each (revisionsQuery.data?.items ?? []) as rev (rev.id)}
                <option value={rev.id}>{rev.label}</option>
              {/each}
            </Select>
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" onclick={() => { dialogOpen = false; }}>Cancel</Button>
            <Button disabled={!step1Valid} onclick={() => { step = 2; }}>Next</Button>
          </div>
        </div>

      {:else if step === 2}
        <div class="space-y-3">
          <div class="space-y-1">
            <Label for="build-qty">Quantity to Build <span class="text-destructive">*</span></Label>
            <Input
              id="build-qty"
              type="number"
              bind:value={buildQty}
              min="1"
              placeholder="e.g. 10"
            />
          </div>
          <div class="space-y-1">
            <Label for="build-notes">Notes</Label>
            <Input
              id="build-notes"
              bind:value={buildNotes}
              placeholder="Optional notes"
            />
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" onclick={() => { step = 1; }}>Back</Button>
            <Button
              disabled={!step2Valid() || previewing}
              onclick={handlePreview}
            >
              {previewing ? 'Checking…' : 'Preview'}
            </Button>
          </div>
        </div>

      {:else if step === 3}
        <div class="space-y-3">
          <p class="text-sm text-muted-foreground">
            Parts to be deducted for <strong>{buildQty}</strong> board(s):
          </p>
          <div class="max-h-64 overflow-y-auto rounded border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Part MPN</TableHead>
                  <TableHead>Per Board</TableHead>
                  <TableHead>Total</TableHead>
                  <TableHead>On Hand</TableHead>
                  <TableHead>OK?</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each previewItems as pi (pi.bom_line_item_id)}
                  <TableRow>
                    <TableCell class="font-mono text-xs">{pi.resolved_part_mpn ?? '—'}</TableCell>
                    <TableCell>{pi.quantity_per_board}</TableCell>
                    <TableCell>{pi.total_quantity}</TableCell>
                    <TableCell>{pi.quantity_on_hand}</TableCell>
                    <TableCell>
                      <Badge variant={pi.sufficient ? 'success' : 'destructive'}>
                        {pi.sufficient ? 'OK' : 'Short'}
                      </Badge>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
          {#if previewItems.some(pi => !pi.sufficient)}
            <p class="text-sm text-warning text-yellow-600">
              Warning: some parts have insufficient stock. Build will proceed anyway.
            </p>
          {/if}
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" onclick={() => { step = 2; }}>Back</Button>
            <Button onclick={handleConfirmBuild} disabled={submitting}>
              {submitting ? 'Recording…' : 'Confirm Build'}
            </Button>
          </div>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

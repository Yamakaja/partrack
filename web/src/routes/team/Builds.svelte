<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listBuilds, createBuild } from '$lib/api/builds.js';
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
  import type { BuildStatus } from '$lib/types/orders.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let limit = $state(50);
  let offset = $state(0);
  let statusFilter = $state('');

  let buildsQuery = createQuery(() => ({
    queryKey: ['builds', teamId, statusFilter, limit, offset],
    queryFn: () => listBuilds(teamId, { status: statusFilter || undefined, limit, offset }),
  }));

  // Dialog state
  let dialogOpen = $state(false);
  let step = $state(1);
  let selectedProjectId = $state('');
  let selectedRevisionId = $state('');
  let buildQty = $state('1');
  let buildNotes = $state('');
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

  $effect(() => {
    selectedRevisionId = revisionsQuery.data?.items.filter(item => item.is_active)[0]?.id ?? ""
  })

  function openDialog() {
    step = 1;
    selectedProjectId = '';
    selectedRevisionId = '';
    buildQty = '1';
    buildNotes = '';
    dialogOpen = true;
  }

  async function handleCreate() {
    const qty = parseInt(buildQty, 10);
    if (!selectedProjectId || !selectedRevisionId || isNaN(qty) || qty < 1) return;
    submitting = true;
    try {
      const build = await createBuild(teamId, {
        project_id: selectedProjectId,
        bom_revision_id: selectedRevisionId,
        quantity: qty,
        notes: buildNotes.trim() || undefined,
      });
      dialogOpen = false;
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      navigate(`/teams/${teamId}/builds/${build.id}`);
    } catch (err: any) {
      pushToast('error', 'Failed to create build plan', err.message);
    } finally {
      submitting = false;
    }
  }

  let step1Valid = $derived(!!selectedProjectId && !!selectedRevisionId);
  let step2Valid = $derived.by(() => {
    const q = parseInt(buildQty, 10);
    return !isNaN(q) && q >= 1;
  });

  function statusVariant(status: BuildStatus): 'secondary' | 'default' | 'warning' | 'success' | 'destructive' {
    switch (status) {
      case 'draft': return 'secondary';
      case 'pending': return 'default';
      case 'ordered': return 'warning';
      case 'ready': return 'success';
      case 'assembled': return 'secondary';
      case 'cancelled': return 'destructive';
      default: return 'secondary';
    }
  }

  function statusLabel(status: BuildStatus): string {
    switch (status) {
      case 'draft': return 'Draft';
      case 'pending': return 'Pending';
      case 'ordered': return 'Ordered';
      case 'ready': return 'Ready';
      case 'assembled': return 'Assembled';
      case 'cancelled': return 'Cancelled';
      default: return status;
    }
  }

  const filterOptions = [
    { value: '', label: 'All' },
    { value: 'draft', label: 'Draft' },
    { value: 'pending', label: 'Pending' },
    { value: 'ordered', label: 'Ordered' },
    { value: 'ready', label: 'Ready' },
    { value: 'assembled', label: 'Assembled' },
    { value: 'cancelled', label: 'Cancelled' },
  ];

  let readyCount = $derived(
    (buildsQuery.data?.items ?? []).filter(b => b.status === 'ready').length
  );
  let waitingCount = $derived(
    (buildsQuery.data?.items ?? []).filter(b => b.status === 'pending' || b.status === 'ordered').length
  );
</script>

<div class="flex flex-col h-full">
  <Topbar title="Builds">
    {#snippet actions()}
      <Button size="sm" onclick={openDialog}>
        <Plus class="h-4 w-4 mr-1" /> Create Build Plan
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <!-- Status filter -->
    <div class="flex items-center gap-1.5 flex-wrap">
      {#each filterOptions as opt}
        <button
          class="px-3 py-1 rounded-full text-xs font-medium transition-colors border {statusFilter === opt.value ? 'bg-foreground text-background border-foreground' : 'border-border text-muted-foreground hover:text-foreground hover:border-foreground/40'}"
          onclick={() => { statusFilter = opt.value; offset = 0; }}
        >
          {opt.label}
        </button>
      {/each}
    </div>

    {#if buildsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading builds…</div>
    {:else if buildsQuery.isError}
      <div class="text-sm text-destructive">Error: {buildsQuery.error.message}</div>
    {:else if buildsQuery.data}
      {@const data = buildsQuery.data}

      <!-- Pipeline summary (shown on unfiltered view) -->
      {#if !statusFilter && (readyCount > 0 || waitingCount > 0)}
        <div class="flex items-center gap-4 text-sm">
          {#if readyCount > 0}
            <span class="flex items-center gap-1.5">
              <Badge variant="success">{readyCount}</Badge>
              build{readyCount !== 1 ? 's' : ''} ready to assemble
            </span>
          {/if}
          {#if waitingCount > 0}
            <span class="flex items-center gap-1.5">
              <Badge variant="warning">{waitingCount}</Badge>
              build{waitingCount !== 1 ? 's' : ''} waiting for parts
            </span>
          {/if}
        </div>
      {/if}

      {#if data.items.length === 0}
        <EmptyState
          title="No builds found"
          description={statusFilter ? `No ${statusFilter} builds.` : 'Create a build plan to start tracking PCB assembly.'}
        >
          {#snippet action()}
            {#if !statusFilter}
              <Button size="sm" onclick={openDialog}>
                <Plus class="h-4 w-4 mr-1" /> Create Build Plan
              </Button>
            {/if}
          {/snippet}
        </EmptyState>
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Project</TableHead>
              <TableHead>Revision</TableHead>
              <TableHead>Quantity</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created</TableHead>
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
                <TableCell class="tabular-nums">{build.quantity}</TableCell>
                <TableCell>
                  <Badge variant={statusVariant(build.status)}>{statusLabel(build.status)}</Badge>
                </TableCell>
                <TableCell>
                  <DateDisplay value={build.created_at} />
                </TableCell>
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

<!-- Create Build Plan Dialog -->
<Dialog.Root bind:open={dialogOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Create Build Plan</Dialog.Title>
      <p class="text-sm text-muted-foreground mb-4">Step {step} of 2</p>

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
            <Dialog.Close>
              <Button variant="outline" type="button">Cancel</Button>
            </Dialog.Close>
            <Button disabled={!step1Valid} onclick={() => { step = 2; }}>Next</Button>
          </div>
        </div>

      {:else if step === 2}
        <div class="space-y-3">
          <div class="space-y-1">
            <Label for="build-qty">Quantity <span class="text-destructive">*</span></Label>
            <Input
              id="build-qty"
              type="number"
              bind:value={buildQty}
              min="1"
              placeholder="e.g. 10"
              onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter' && step2Valid) handleCreate(); }}
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
              disabled={!step2Valid || submitting}
              onclick={handleCreate}
            >
              {submitting ? 'Creating…' : 'Create Build Plan'}
            </Button>
          </div>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

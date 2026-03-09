<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listBuilds, createBuild, getBuildPipeline } from '$lib/api/builds.js';
  import { listProjects } from '$lib/api/projects.js';
  import { listRevisions } from '$lib/api/revisions.js';
  import { listMembers } from '$lib/api/teams.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import UserPicker from '$lib/components/common/UserPicker.svelte';
  import { DataTable, RemoteDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { Plus, Filter } from 'lucide-svelte';
  import type { Build, BuildStatus } from '$lib/types/orders.js';
  import type { User } from '$lib/types/auth.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  // ── Pipeline ────────────────────────────────────────────────────
  const pipelineQuery = createQuery(() => ({
    queryKey: ['builds-pipeline', teamId],
    queryFn: () => getBuildPipeline(teamId),
  }));

  const currentUserId = $derived(authStore.user?.id);

  function sortUserFirst(builds: Build[]): Build[] {
    return [...builds].sort((a, b) => {
      const aMe = a.created_by === currentUserId ? 0 : 1;
      const bMe = b.created_by === currentUserId ? 0 : 1;
      return aMe - bMe;
    });
  }

  const draftBuilds   = $derived(sortUserFirst(pipelineQuery.data?.draft   ?? []));
  const pendingBuilds = $derived(sortUserFirst(pipelineQuery.data?.pending ?? []));
  const orderedBuilds = $derived(sortUserFirst(pipelineQuery.data?.ordered ?? []));
  const readyBuilds   = $derived(sortUserFirst(pipelineQuery.data?.ready   ?? []));

  const pipelineCards = $derived([
    { status: 'draft'   as BuildStatus, label: 'Draft',   builds: draftBuilds,   empty: 'No drafts' },
    { status: 'pending' as BuildStatus, label: 'Pending', builds: pendingBuilds, empty: 'Nothing pending' },
    { status: 'ordered' as BuildStatus, label: 'Ordered', builds: orderedBuilds, empty: 'No active orders' },
    { status: 'ready'   as BuildStatus, label: 'Ready',   builds: readyBuilds,   empty: 'All assembled' },
  ]);

  // ── All Builds table ─────────────────────────────────────────────
  let creatorFilter = $state<string[]>([]);  // single-select via UserPicker

  const membersQuery = createQuery(() => ({
    queryKey: ['members', teamId],
    queryFn: () => listMembers(teamId),
  }));

  // Convert TeamMember[] → User[] for UserPicker
  const memberUsers = $derived<User[]>(
    (membersQuery.data?.items ?? []).map((m) => ({
      id:           m.user_id,
      display_name: m.display_name,
      email:        m.email,
      is_active:    true,
      is_admin:     false,
      created_at:   '',
    })),
  );

  const columns: ColumnSpec<Build>[] = [
    { key: 'project_name',            header: 'Project',  type: 'text',    sortable: true },
    { key: 'bom_revision_label',      header: 'Revision', type: 'text',    sortable: true },
    { key: 'quantity',                header: 'Qty',      type: 'numeric', sortable: true, align: 'right' },
    {
      key: 'status', header: 'Status', type: 'enum', sortable: true,
      filterable: true,
      filterValues: ['draft','pending','ordered','ready','assembled','cancelled'],
      filterLabels: {
        draft: 'Draft', pending: 'Pending', ordered: 'Ordered',
        ready: 'Ready', assembled: 'Assembled', cancelled: 'Cancelled',
      },
    },
    { key: 'created_at',              header: 'Created',  type: 'date', sortable: true },
    { key: 'created_by_display_name', header: 'Creator',  type: 'text', sortable: false },
    { key: 'notes',                   header: 'Notes',    type: 'text', sortable: false },
  ];

  const source = new RemoteDataSource<Build>(
    columns,
    async (params) => {
      const statuses = [...(params.filters.get('status') ?? [])];
      const creatorId = (params.extra.creatorFilter as string[])[0] ?? undefined;
      return listBuilds(teamId, {
        status:     statuses.length ? statuses : undefined,
        created_by: creatorId,
        sort_by:    params.sortKey ?? undefined,
        sort_dir:   params.sortDirection,
        limit:      params.pageSize,
        offset:     (params.page - 1) * params.pageSize,
      });
    },
    () => ({ creatorFilter }),
    { pageSize: 50 },
  );

  // ── Create Build Plan dialog ──────────────────────────────────────
  let dialogOpen = $state(false);
  let step = $state(1);
  let selectedProjectId = $state('');
  let selectedRevisionId = $state('');
  let buildQty = $state('1');
  let buildNotes = $state('');
  let submitting = $state(false);

  const projectsQuery = createQuery(() => ({
    queryKey: ['projects', teamId],
    queryFn: () => listProjects(teamId),
    enabled: dialogOpen,
  }));

  const revisionsQuery = createQuery(() => ({
    queryKey: ['revisions', teamId, selectedProjectId],
    queryFn: () => listRevisions(teamId, selectedProjectId),
    enabled: dialogOpen && !!selectedProjectId,
  }));

  $effect(() => {
    selectedRevisionId = revisionsQuery.data?.items.filter(item => item.is_active)[0]?.id ?? '';
  });

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
        project_id:      selectedProjectId,
        bom_revision_id: selectedRevisionId,
        quantity:        qty,
        notes:           buildNotes.trim() || undefined,
      });
      dialogOpen = false;
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['builds-pipeline', teamId] });
      navigate(`/teams/${teamId}/builds/${build.id}`);
    } catch (err: any) {
      pushToast('error', 'Failed to create build plan', err.message);
    } finally {
      submitting = false;
    }
  }

  const step1Valid = $derived(!!selectedProjectId && !!selectedRevisionId);
  const step2Valid = $derived.by(() => {
    const q = parseInt(buildQty, 10);
    return !isNaN(q) && q >= 1;
  });

  function statusVariant(status: BuildStatus): 'secondary' | 'default' | 'warning' | 'success' | 'destructive' {
    switch (status) {
      case 'draft':     return 'secondary';
      case 'pending':   return 'default';
      case 'ordered':   return 'warning';
      case 'ready':     return 'success';
      case 'assembled': return 'secondary';
      case 'cancelled': return 'destructive';
      default:          return 'secondary';
    }
  }

  function statusLabel(status: BuildStatus): string {
    switch (status) {
      case 'draft':     return 'Draft';
      case 'pending':   return 'Pending';
      case 'ordered':   return 'Ordered';
      case 'ready':     return 'Ready';
      case 'assembled': return 'Assembled';
      case 'cancelled': return 'Cancelled';
      default:          return status;
    }
  }
</script>

<!-- Custom cell renderers (top-level snippets, referenced in DataTable renderers prop) -->
{#snippet renderStatus(value: unknown, _row: Build)}
  <Badge variant={statusVariant(value as BuildStatus)}>{statusLabel(value as BuildStatus)}</Badge>
{/snippet}

{#snippet renderDate(value: unknown, _row: Build)}
  <DateDisplay value={value as string} />
{/snippet}

{#snippet renderNotes(value: unknown, _row: Build)}
  {#if value}
    <span class="truncate block max-w-[200px] text-muted-foreground text-xs" title={value as string}>
      {value as string}
    </span>
  {:else}
    <span class="text-muted-foreground">—</span>
  {/if}
{/snippet}

{#snippet creatorHeaderFilter()}
  <UserPicker
    users={memberUsers}
    selected={creatorFilter}
    onToggle={(id) => { creatorFilter = creatorFilter[0] === id ? [] : [id]; }}
    onClear={() => { creatorFilter = []; }}
  >
    {#snippet trigger({ open: _open })}
      <button
        class="rounded p-0.5 hover:bg-accent transition-colors {creatorFilter.length > 0 ? 'text-primary' : 'text-muted-foreground opacity-60 hover:opacity-100'}"
        aria-label="Filter Creator"
      >
        <Filter class="h-3 w-3" />
      </button>
    {/snippet}
  </UserPicker>
{/snippet}

{#snippet renderCreator(value: unknown, row: Build)}
  {#if row.created_by === currentUserId}
    <span class="flex items-center gap-1">
      <span class="text-primary text-[10px]">●</span>
      <span>You</span>
    </span>
  {:else}
    <span>{(value as string | null) ?? '—'}</span>
  {/if}
{/snippet}

<div class="flex flex-col h-full">
  <Topbar title="Builds">
    {#snippet actions()}
      <Button size="sm" onclick={openDialog}>
        <Plus class="h-4 w-4 mr-1" /> Create Build Plan
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">

    <!-- ── Active Builds Pipeline ──────────────────────────────── -->
    <section>
      <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">
        Active Builds
      </h3>
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {#each pipelineCards as card}
          <div class="rounded-lg border border-border bg-card flex flex-col">
            <!-- Card header -->
            <div class="flex items-center justify-between px-3 py-2 border-b border-border">
              <span class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {card.label}
              </span>
              {#if card.builds.length > 0}
                <Badge variant={statusVariant(card.status)} class="text-xs px-1.5 py-0 h-4">
                  {card.builds.length}
                </Badge>
              {/if}
            </div>
            <!-- Card body -->
            <div class="overflow-y-auto max-h-60 p-1.5 flex-1">
              {#if card.builds.length === 0}
                <p class="text-xs text-muted-foreground text-center py-4">{card.empty}</p>
              {:else}
                {#each card.builds as build (build.id)}
                  {@const isMe = build.created_by === currentUserId}
                  {@const muted = card.status === 'pending' && build.excluded_from_cart}
                  <button
                    class="w-full text-left rounded px-2 py-1.5 text-xs hover:bg-muted transition-colors flex items-start gap-1 {muted ? 'text-muted-foreground italic' : ''}"
                    onclick={() => navigate(`/teams/${teamId}/builds/${build.id}`)}
                  >
                    {#if isMe}
                      <span class="text-primary mt-px shrink-0" title="Your build">●</span>
                    {:else}
                      <span class="shrink-0 w-2"></span>
                    {/if}
                    <span class="flex-1 min-w-0">
                      <span class="flex items-baseline justify-between gap-1">
                        <span class="font-medium truncate">{build.project_name}</span>
                        <span class="text-muted-foreground/70 shrink-0 text-[10px]">
                          {isMe ? 'You' : (build.created_by_display_name ?? '—')}
                        </span>
                      </span>
                      <span class="text-muted-foreground truncate block">
                        {build.bom_revision_label} · {build.quantity}×
                      </span>
                    </span>
                  </button>
                {/each}
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </section>

    <!-- ── All Builds Table ────────────────────────────────────── -->
    <section>
      <DataTable
        {source}
        getRowKey={(b) => b.id}
        onRowClick={(b) => navigate(`/teams/${teamId}/builds/${b.id}`)}
        renderers={{ status: renderStatus, created_at: renderDate, created_by_display_name: renderCreator, notes: renderNotes }}
        headerFilters={{ created_by_display_name: creatorHeaderFilter }}
        exportFilename="builds.csv"
      />
    </section>

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
            <Select id="build-project" bind:value={selectedProjectId}>
              <option value="">Select project…</option>
              {#each (projectsQuery.data?.items ?? []) as project (project.id)}
                <option value={project.id}>{project.name}</option>
              {/each}
            </Select>
          </div>
          <div class="space-y-1">
            <Label for="build-revision">BOM Revision <span class="text-destructive">*</span></Label>
            <Select id="build-revision" bind:value={selectedRevisionId} disabled={!selectedProjectId}>
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
            <Input id="build-notes" bind:value={buildNotes} placeholder="Optional notes" />
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" onclick={() => { step = 1; }}>Back</Button>
            <Button disabled={!step2Valid || submitting} onclick={handleCreate}>
              {submitting ? 'Creating…' : 'Create Build Plan'}
            </Button>
          </div>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getProject } from '$lib/api/projects.js';
  import type { BOMLineItem } from '$lib/types/projects.js';
  import { getRevision, updateBOMLineItem, updateRevision } from '$lib/api/revisions.js';
  import { createGenericPart } from '$lib/api/parts.js';
  import { listCategories } from '$lib/api/categories.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import { onMount } from 'svelte';
  import BOMImportProgress from '$lib/components/bom/BOMImportProgress.svelte';
  import BOMImportProgressModal from '$lib/components/bom/BOMImportProgressModal.svelte';
  import BOMResolutionBar from '$lib/components/bom/BOMResolutionBar.svelte';
  import BOMReviewDialog from '$lib/components/bom/BOMReviewDialog.svelte';
  import ManageSubstitutesDialog from '$lib/components/bom/ManageSubstitutesDialog.svelte';
  import StockCheckDialog from '$lib/components/stock/StockCheckDialog.svelte';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { ClipboardCheck, Pencil, Shuffle } from 'lucide-svelte';
  import { formatDate } from '$lib/utils/date.js';

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

  // Modal triggered by ?import_job query param when navigating from BOMImportDialog
  let showImportModal = $state(false);
  let modalJobId = $state('');

  onMount(() => {
    const jobParam = new URLSearchParams(window.location.search).get('import_job');
    if (jobParam) {
      modalJobId = jobParam;
      showImportModal = true;
    }
  });

  // Review dialog state
  let reviewOpen = $state(false);
  let reviewItems = $state<BOMLineItem[]>([]);

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let revisionQuery = createQuery(() => ({
    queryKey: ['revision', teamId, projectId, revId],
    queryFn: () => getRevision(teamId, projectId, revId),
  }));

  let categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: () => listCategories(),
  }));
  let categoryIdBySlug = $derived.by(() => {
    const m = new Map<string, number>();
    for (const c of categoriesQuery.data ?? []) m.set(c.slug, c.id);
    return m;
  });

  let allItems = $derived(revisionQuery.data?.items ?? []);

  let isReady = $derived(
    allItems.length > 0 && allItems.every(i => i.resolved_part_id != null || i.ignored)
  );

  // Items the user can act on now — excludes parts still importing in the background.
  function isReviewable(i: BOMLineItem): boolean {
    return !i.ignored && !i.resolved_part_id && i.resolution_state !== 'importing';
  }

  // A fuzzy-MPN auto-resolution is resolved but uncertain — surface it for a
  // confirm even though it has a part. These are the only resolved lines that
  // still warrant review.
  function needsReview(i: BOMLineItem): boolean {
    return isReviewable(i) || (!i.ignored && !!i.resolved_part_id && i.resolution_state === 'auto_mpn_fuzzy');
  }

  let needsAttentionCount = $derived(allItems.filter(needsReview).length);

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

  async function handleIgnore(itemId: string, ignored: boolean) {
    try {
      await updateBOMLineItem(teamId, projectId, revId, itemId, { ignored });
      reviewItems = reviewItems.map(i => i.id === itemId ? { ...i, ignored } : i);
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
      pushToast('success', ignored ? 'Item ignored' : 'Item un-ignored');
    } catch (err: any) {
      pushToast('error', 'Failed to update line item', err.message);
    }
  }

  function startFullReview() {
    reviewItems = allItems.filter(needsReview);
    reviewOpen = true;
  }

  function startReviewForItem(itemId: string) {
    const item = allItems.find(i => i.id === itemId);
    reviewItems = item ? [item] : [];
    reviewOpen = true;
  }

  function handleReviewClose() {
    reviewOpen = false;
  }

  // Unresolved rows the parser read as generic specs — eligible for one-click
  // generic-part creation (create-or-get).
  let genericCandidates = $derived(
    allItems.filter((i) => isReviewable(i) && i.interpretation?.route === 'parametric'),
  );

  let creatingGeneric = $state(false);

  async function createGenericForItem(item: BOMLineItem): Promise<boolean> {
    const interp = item.interpretation;
    if (!interp || interp.route !== 'parametric') return false;
    const categoryId = categoryIdBySlug.get(interp.category);
    if (!categoryId) {
      pushToast('error', 'Unknown category', interp.category || '(none)');
      return false;
    }
    // Package is a parameter (slug "package"); pass it via the dedicated field and
    // exclude it from the generic params list so it isn't duplicated.
    const res = await createGenericPart({
      team_id: teamId,
      category_id: categoryId,
      package: (interp.params ?? []).find((p) => p.slug === 'package')?.raw || undefined,
      params: (interp.params ?? [])
        .filter((p) => p.slug !== 'package')
        .map((p) => ({ slug: p.slug, value: p.raw })),
    });
    await updateBOMLineItem(teamId, projectId, revId, item.id, { resolved_part_id: res.part_id });
    return true;
  }

  // Used by the review dialog: create-or-get a generic part and resolve the line,
  // returning success so the dialog can advance. Network errors propagate so the
  // dialog can show them inline.
  async function handleReviewCreateGeneric(item: BOMLineItem): Promise<boolean> {
    const ok = await createGenericForItem(item);
    if (ok) {
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
      pushToast('success', 'Generic part created');
    }
    return ok;
  }

  async function createOneGeneric(item: BOMLineItem) {
    if (creatingGeneric) return;
    creatingGeneric = true;
    try {
      if (await createGenericForItem(item)) {
        qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
        pushToast('success', 'Generic part created');
      }
    } catch (err: any) {
      pushToast('error', 'Failed to create generic part', err.message);
    } finally {
      creatingGeneric = false;
    }
  }

  async function createAllGenerics() {
    if (creatingGeneric || genericCandidates.length === 0) return;
    creatingGeneric = true;
    let n = 0;
    try {
      for (const item of genericCandidates) {
        try {
          if (await createGenericForItem(item)) n++;
        } catch {
          /* keep going; failures are reported in aggregate */
        }
      }
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
      pushToast('success', `Linked ${n} generic part${n === 1 ? '' : 's'}`);
    } finally {
      creatingGeneric = false;
    }
  }

  // Substitutes dialog state
  let managingItem = $state<BOMLineItem | null>(null);
  let manageSubsOpen = $state(false);

  function openManageSubstitutes(item: BOMLineItem) {
    managingItem = item;
    manageSubsOpen = true;
  }

  // Inline editing
  let editing = $state(false);
  let editLabel = $state('');
  let editNotes = $state('');
  let editSaving = $state(false);

  function openEdit() {
    const rev = revisionQuery.data;
    if (!rev) return;
    editLabel = rev.label;
    editNotes = rev.notes ?? '';
    editing = true;
  }

  async function handleEditSave(e: Event) {
    e.preventDefault();
    editSaving = true;
    try {
      await updateRevision(teamId, projectId, revId, {
        label: editLabel.trim() || undefined,
        notes: editNotes.trim() || undefined,
      });
      editing = false;
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
      pushToast('success', 'Revision updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update revision', err.message);
    } finally {
      editSaving = false;
    }
  }

  let projectName = $derived(projectQuery.data?.name ?? 'Project');
  let revisionLabel = $derived(revisionQuery.data?.label ?? 'Revision');

  // DataTable setup
  type BOMRow = BOMLineItem & { status: string };

  function itemStatus(item: BOMLineItem): string {
    if (item.ignored) return 'Ignored';
    if (!item.resolved_part_id) {
      if (item.resolution_state === 'importing') return 'Importing';
      if (item.resolution_state === 'import_ambiguous' || item.resolution_state === 'ambiguous') return 'Needs Review';
      if (item.resolution_state === 'import_no_result') return 'No Result';
      return 'Unresolved';
    }
    if (item.resolution_state === 'auto_mpn_fuzzy') return 'Auto (MPN ?)';
    if (item.resolution_state === 'auto_sku') return 'Auto (SKU)';
    if (item.resolution_state === 'auto_mpn') return 'Auto (MPN)';
    if (item.resolution_state === 'auto_generic') return 'Auto (Generic)';
    if (item.resolution_state === 'auto_prior_revision') return 'Auto (Prior)';
    if (item.resolution_state === 'auto_import') return 'Imported';
    if (item.resolution_state === 'manual') return 'Manual';
    return 'Resolved';
  }

  let rows = $derived(allItems.map(i => ({ ...i, status: itemStatus(i) })));

  const columns: ColumnSpec<BOMRow>[] = [
    { key: 'part_name',             header: 'Name',             type: 'text',    filterable: true },
    { key: 'manufacturer',          header: 'Manufacturer',     type: 'text',    filterable: true },
    { key: 'mpn',                   header: 'MPN',              type: 'text',    filterable: true },
    { key: 'raw_description',       header: 'Description',      type: 'text' },
    { key: 'reference_designators', header: 'Ref. Designators', type: 'text' },
    { key: 'distributor_name',      header: 'Distributor',      type: 'text',    filterable: true },
    { key: 'sku',                   header: 'SKU',              type: 'text' },
    { key: 'quantity',              header: 'Qty',              type: 'numeric', align: 'right', sortable: true },
    {
      key: 'status',
      header: 'Status',
      type: 'enum',
      filterable: true,
      filterValues: ['Unresolved', 'Importing', 'Needs Review', 'Auto (MPN ?)', 'No Result', 'Auto (SKU)', 'Auto (MPN)', 'Auto (Generic)', 'Auto (Prior)', 'Imported', 'Manual', 'Resolved', 'Ignored'],
    },
    { key: 'substitutes', header: 'Substitutes', type: 'text' },
  ];

  const source = new LocalDataSource<BOMRow>(() => rows, columns);
</script>

<div class="flex flex-col h-full">
  <Topbar
    title={revisionLabel}
    breadcrumbs={[
      { label: 'Projects', href: `/teams/${teamId}/projects` },
      { label: projectName, href: `/teams/${teamId}/projects/${projectId}` },
    ]}
  >
    {#snippet badges()}
      {#if revisionQuery.data}
        {#if revisionQuery.data.is_active}<Badge variant="success">Active BOM</Badge>{/if}
        {#if isReady}<Badge variant="success">Ready</Badge>{:else}<Badge variant="warning">Unresolved Items</Badge>{/if}
      {/if}
    {/snippet}
    {#snippet actions()}
      {#if allItems.length > 0}
        {#if needsAttentionCount > 0}
          <Button
            size="sm"
            onclick={startFullReview}
            class="bg-amber-500 text-white hover:bg-amber-600 dark:bg-amber-600 dark:hover:bg-amber-700"
          >
            {needsAttentionCount} {needsAttentionCount === 1 ? 'item' : 'items'} ready for review
          </Button>
        {/if}
        {#if genericCandidates.length > 0}
          <Button size="sm" variant="outline" onclick={createAllGenerics} disabled={creatingGeneric}>
            {creatingGeneric ? 'Creating…' : `Create ${genericCandidates.length} generic part${genericCandidates.length === 1 ? '' : 's'}`}
          </Button>
        {/if}
      {/if}

      <StockCheckDialog {teamId} {projectId} {revId}>
        {#snippet trigger(open)}
          <Button size="sm" onclick={open}>
            <ClipboardCheck class="h-4 w-4 mr-1" /> Check Stock
          </Button>
        {/snippet}
      </StockCheckDialog>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <!-- Import progress banner (fallback for jobs not initiated via the dialog) -->
    {#if activeJobId}
      <BOMImportProgress
        {teamId}
        jobId={activeJobId}
        oncomplete={handleImportComplete}
      />
    {/if}

    <!-- Live resolution breakdown (updates as background imports commit) -->
    {#if allItems.length > 0}
      <BOMResolutionBar items={allItems} />
    {/if}

    <!-- Revision metadata card -->
    {#if revisionQuery.data}
      {@const rev = revisionQuery.data}
      <MetadataCard
        title={editing ? '' : rev.label}
        description={(!editing && rev.notes) ? rev.notes : undefined}
      >
        {#snippet right()}
          {#if !editing}
            <Button variant="outline" size="sm" onclick={openEdit}>
              <Pencil class="h-4 w-4 mr-1" /> Edit
            </Button>
          {/if}
        {/snippet}

        {#if editing}
          <form onsubmit={handleEditSave} class="space-y-3">
            <div class="space-y-1">
              <Label class="text-xs">Label</Label>
              <Input bind:value={editLabel} class="h-8 text-sm max-w-xs" />
            </div>
            <div class="space-y-1">
              <Label class="text-xs">Notes</Label>
              <textarea
                bind:value={editNotes}
                rows={3}
                placeholder="Optional notes…"
                class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm resize-none focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              ></textarea>
            </div>
            <div class="flex gap-2">
              <Button type="submit" size="sm" disabled={editSaving}>
                {editSaving ? 'Saving…' : 'Save'}
              </Button>
              <Button type="button" variant="outline" size="sm" onclick={() => { editing = false; }}>
                Cancel
              </Button>
            </div>
          </form>
        {:else}
          <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm">
            <div>
              <span class="text-muted-foreground">Imported by:</span>
              <span class="font-medium ml-1">{rev.imported_by_display_name}</span>
            </div>
            <div>
              <span class="text-muted-foreground">Imported:</span>
              <span class="font-medium ml-1">{formatDate(rev.imported_at)}</span>
            </div>
          </div>
        {/if}
      </MetadataCard>
    {/if}

    <!-- BOM line items table -->
    {#if revisionQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if revisionQuery.isError}
      <div class="text-sm text-destructive">Error: {revisionQuery.error.message}</div>
    {:else if revisionQuery.data}
      {#if allItems.length === 0}
        <EmptyState
          title="No line items"
          description="This revision has no BOM line items."
        />
      {:else}
        <DataTable
          {source}
          getRowKey={(row) => row.id}
          onRowClick={(row) => startReviewForItem(row.id)}
          rowClass={(row) => row.ignored ? 'opacity-50' : ''}
          renderers={{ status: renderStatus, distributor_name: renderDistributor, reference_designators: renderRefDes, substitutes: renderSubstitutes }}
          emptyMessage="No line items match the current filter"
        />
      {/if}
    {/if}
  </div>
</div>

{#snippet renderStatus(value: unknown, row: BOMRow)}
  {@const statusVariant = (() => {
    switch (value as string) {
      case 'Ignored':      return 'secondary' as const;
      case 'Ambiguous':
      case 'Needs Review':
      case 'Auto (MPN ?)':  return 'warning' as const;
      case 'Unresolved':   return 'outline' as const;
      default:             return 'success' as const;
    }
  })()}
  <div class="inline-flex items-center gap-1.5">
    <Badge variant={statusVariant}>{value as string}</Badge>
    {#if row.resolved_part_id && row.resolved_part_mpn}
      <a
        use:linkAction
        href="/teams/{teamId}/parts/{row.resolved_part_id}"
        onclick={(e) => e.stopPropagation()}
        class="font-mono text-xs text-muted-foreground hover:underline"
      >{row.resolved_part_mpn}</a>
    {/if}
    {#if isReviewable(row) && row.interpretation?.route === 'parametric'}
      <Button
        size="sm"
        variant="outline"
        class="h-6 px-2 text-xs"
        disabled={creatingGeneric}
        onclick={(e: MouseEvent) => { e.stopPropagation(); createOneGeneric(row); }}
      >Create generic</Button>
    {/if}
  </div>
{/snippet}

{#snippet renderDistributor(value: unknown, row: BOMRow)}
  {#if value}
    <div class="flex items-center gap-1">
      <span>{value as string}</span>
      <span
        class="text-xs"
        title={row.has_linked_distributor ? 'Distributor linked' : 'Distributor not linked'}
      >{row.has_linked_distributor ? '✓' : '✕'}</span>
    </div>
  {/if}
{/snippet}

{#snippet renderRefDes(value: unknown, _row: BOMRow)}
  {@const s = value as string ?? ''}
  <span class="font-mono text-xs text-muted-foreground">
    {s.length > 80 ? s.slice(0, 80) + '…' : (s || '—')}
  </span>
{/snippet}

{#snippet renderSubstitutes(_value: unknown, row: BOMRow)}
  <div class="flex items-center gap-1.5">
    {#if row.substitutes && row.substitutes.length > 0}
      <Badge variant="secondary" class="text-xs">{row.substitutes.length}</Badge>
    {/if}
    {#if row.resolved_part_id}
      <Button
        variant="ghost"
        size="sm"
        class="h-7 px-2 text-xs"
        onclick={(e: MouseEvent) => { e.stopPropagation(); openManageSubstitutes(row); }}
      >
        <Shuffle class="h-3 w-3 mr-1" />
        {row.substitutes && row.substitutes.length > 0 ? 'Manage' : 'Add'}
      </Button>
    {/if}
  </div>
{/snippet}

{#if showImportModal}
  <BOMImportProgressModal
    {teamId}
    jobId={modalJobId}
    onStartReview={async () => {
      await qc.refetchQueries({ queryKey: ['revision', teamId, projectId, revId] });
      showImportModal = false;
      startFullReview();
    }}
    onClose={() => {
      showImportModal = false;
      qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] });
    }}
  />
{/if}

<BOMReviewDialog
  items={reviewItems}
  {teamId}
  {projectId}
  revisionId={revId}
  bind:open={reviewOpen}
  onclose={handleReviewClose}
  onresolve={handleResolve}
  onignore={handleIgnore}
  oncreategeneric={handleReviewCreateGeneric}
  oncommitted={() => qc.invalidateQueries({ queryKey: ['revision', teamId, projectId, revId] })}
/>

{#if managingItem}
  <ManageSubstitutesDialog
    {teamId}
    {projectId}
    revisionId={revId}
    lineItem={managingItem}
    bind:open={manageSubsOpen}
  />
{/if}

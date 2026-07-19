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
  import PartCell from '$lib/components/parts/PartCell.svelte';
  import { listParameterDefinitions } from '$lib/api/parameters.js';
  import {
    designedSpec,
    needsAttention,
    resolutionMechanism,
    stockState,
    trustSignal,
  } from '$lib/utils/interpretation.js';
  import { compareDesignators } from '$lib/utils/designators.js';
  import type { ParameterDefinition } from '$lib/types/parts.js';
  import {
    ArrowRight,
    ClipboardCheck,
    CornerDownRight,
    Pencil,
    Shuffle,
    Sparkles,
    TriangleAlert,
    Check,
  } from 'lucide-svelte';
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

  // ── DataTable setup ───────────────────────────────────────────────────────────
  // The table reads as a derivation: what the design asked for → how we got from
  // there to a part → what that part is → whether we can build it. The raw import
  // columns it replaced (name / manufacturer / MPN / description / SKU / distributor)
  // are empty on any BOM exported from a PCB tool, where the identity lives in the
  // value + footprint fields instead; they moved to the row's detail panel.

  // Parameter vocabulary, for turning parsed constraints into readable pills.
  // Global reference data, so one request serves every category on the page.
  let paramDefsQuery = createQuery(() => ({
    queryKey: ['parameter-definitions'],
    queryFn: () => listParameterDefinitions(),
    staleTime: 30 * 60_000,
  }));
  let paramDefBySlug = $derived(
    new Map<string, ParameterDefinition>((paramDefsQuery.data ?? []).map((d) => [d.slug, d])),
  );
  let lookupParam = $derived((slug: string) => paramDefBySlug.get(slug));

  type BOMRow = BOMLineItem & {
    /** Flattened spec text — gives sort, filter and CSV export something to bite on. */
    designed: string;
    mechanism: string;
    stock: number | null;
    attention: boolean;
  };

  // Schematic order (R9 before R10, all the Rs together). The query returns rows in
  // insertion order, which reads as random once the table is scanned as a list.
  let rows = $derived(
    allItems
      .map((i) => {
        const spec = designedSpec(i, lookupParam);
        const designed = [...spec.pills.map((p) => p.value), spec.pkg].filter(Boolean).join(' · ');
        return {
          ...i,
          designed,
          mechanism: resolutionMechanism(i).label,
          stock: i.quantity_on_hand,
          attention: needsAttention(i),
        };
      })
      .sort((a, b) => compareDesignators(a.reference_designators, b.reference_designators)),
  );

  const columns: ColumnSpec<BOMRow>[] = [
    { key: 'reference_designators', header: 'Refs',        type: 'text' },
    { key: 'designed',             header: 'Designed',     type: 'text' },
    { key: 'mechanism',            header: 'How',          type: 'enum',    filterable: true },
    { key: 'resolved_part_mpn',    header: 'Resolved to',  type: 'text' },
    { key: 'stock',                header: 'Stock',        type: 'numeric', align: 'right', sortable: true },
    { key: 'substitutes',          header: 'Substitutes',  type: 'text' },
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
            class="bg-warning text-white hover:bg-warning"
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
          detailPanel={renderDetail}
          renderers={{
            reference_designators: renderRefDes,
            designed: renderDesigned,
            mechanism: renderMechanism,
            resolved_part_mpn: renderResolved,
            stock: renderStock,
            substitutes: renderSubstitutes,
          }}
          emptyMessage="No line items match the current filter"
        />
      {/if}
    {/if}
  </div>
</div>

{#snippet renderRefDes(value: unknown, row: BOMRow)}
  {@const all = (value as string ?? '').split(',').map((s) => s.trim()).filter(Boolean)}
  <div class="font-mono text-xs leading-snug" title={all.join(', ')}>
    {all.slice(0, 3).join(' ')}{#if all.length > 3}<span class="text-muted-foreground"> +{all.length - 3}</span>{/if}
  </div>
  <div class="font-mono text-xs text-muted-foreground">{row.quantity}×</div>
{/snippet}

{#snippet renderDesigned(_value: unknown, row: BOMRow)}
  {@const spec = designedSpec(row, lookupParam)}
  {@const trust = trustSignal(row)}
  <div class="flex flex-wrap items-center gap-1">
    {#each spec.pills as pill}
      <span class="inline-flex items-baseline gap-1 rounded bg-secondary px-1.5 py-0.5 font-mono text-xs max-w-[24ch] truncate">
        {#if pill.label}<span class="text-muted-foreground">{pill.label}</span>{/if}
        {pill.value}
      </span>
    {/each}
    {#if spec.pkg}
      <span
        class="rounded border border-dashed border-border px-1.5 py-0.5 font-mono text-xs text-muted-foreground max-w-[20ch] truncate"
        title={spec.pkg}
      >{spec.pkg}</span>
    {/if}
    {#if trust}
      <span
        class="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium
               {trust.tone === 'success' ? 'bg-success-subtle text-success'
                 : trust.tone === 'info' ? 'bg-info-subtle text-info'
                 : 'bg-warning-subtle text-warning'}"
        title={trust.detail}
      >
        {#if trust.tone === 'success'}<Check class="h-3 w-3" />
        {:else if trust.tone === 'info'}<Sparkles class="h-3 w-3" />
        {:else}<TriangleAlert class="h-3 w-3" />{/if}
        {trust.label}
      </span>
    {/if}
  </div>
  {#if row.value && !spec.echoesValue}
    <div class="mt-0.5 font-mono text-[11px] text-muted-foreground">{row.value}</div>
  {/if}
{/snippet}

{#snippet renderMechanism(_value: unknown, row: BOMRow)}
  {@const m = resolutionMechanism(row)}
  <div class="flex flex-col gap-0.5">
    <span class="inline-flex items-center gap-1.5 text-xs">
      <span
        class="h-1.5 w-1.5 shrink-0 rounded-full
               {m.tone === 'success' ? 'bg-success'
                 : m.tone === 'info' ? 'bg-info'
                 : m.tone === 'warning' ? 'bg-warning'
                 : m.tone === 'attention' ? 'bg-attention'
                 : 'bg-danger'}"
      ></span>
      {m.label}
    </span>
    {#if m.inheritedFrom}
      <span
        class="inline-flex items-center gap-1 font-mono text-[11px] text-muted-foreground"
        title="This revision reused the decision made in {m.inheritedFrom}."
      >
        <CornerDownRight class="h-3 w-3 opacity-70" />
        reused since {m.inheritedFrom}
      </span>
    {/if}
  </div>
{/snippet}

{#snippet renderResolved(_value: unknown, row: BOMRow)}
  {#if row.resolved_part_id}
    <PartCell
      partId={row.resolved_part_id}
      mpn={row.resolved_part_mpn}
      {teamId}
      manufacturer={row.resolved_part_manufacturer}
      description={row.resolved_part_description}
      lifecycle={row.resolved_part_lifecycle_status}
      imageUrl={row.resolved_part_image_url}
      flag={row.resolved_part_flag}
    />
  {:else}
    <button
      type="button"
      class="inline-flex items-center gap-1 text-xs font-medium text-attention hover:underline"
      onclick={(e: MouseEvent) => { e.stopPropagation(); startReviewForItem(row.id); }}
    >
      Resolve <ArrowRight class="h-3 w-3" />
    </button>
  {/if}
{/snippet}

{#snippet renderStock(_value: unknown, row: BOMRow)}
  {@const state = stockState(row)}
  {#if state === null}
    <span class="font-mono text-xs text-muted-foreground">—</span>
  {:else if state === 'untracked'}
    <span class="font-mono text-xs text-muted-foreground" title="No inventory entry for this part">not tracked</span>
  {:else}
    <span
      class="font-mono text-xs whitespace-nowrap
             {state === 'out' ? 'font-medium text-danger'
               : state === 'short' ? 'font-medium text-warning'
               : 'text-muted-foreground'}"
      title={state === 'out' ? 'Out of stock'
        : state === 'short' ? `Only ${row.quantity_on_hand} of ${row.quantity} available`
        : `${row.quantity_on_hand} in stock`}
    >{row.quantity_on_hand} / {row.quantity}</span>
  {/if}
{/snippet}

{#snippet renderDetail(row: BOMRow)}
  {@const interp = row.interpretation}
  <div class="grid gap-x-6 gap-y-3 p-1 sm:grid-cols-2 lg:grid-cols-4">
    {@render detailField('Raw value', row.value)}
    {@render detailField('Footprint', row.footprint)}
    {@render detailField('Class / category', [interp?.class, interp?.category].filter(Boolean).join(' · '))}
    {@render detailField('Route', interp?.route)}
    {@render detailField('Interpretation source', row.interpretation_source)}
    {@render detailField(
      'Reported confidence',
      interp ? `${Math.round(interp.confidence * 100)}%${interp.flags?.includes('llm') || row.interpretation_source === 'llm' ? ' (self-reported)' : ''}` : null,
    )}
    {@render detailField('Flags', interp?.flags?.join(', '))}
    {@render detailField('Resolution state', row.resolution_state)}
    {@render detailField(
      'Inherited from',
      row.origin_revision_label ? `${row.origin_revision_label} (${row.origin_resolution_state})` : null,
    )}
    {@render detailField('Distributor', row.distributor_name)}
    {@render detailField('SKU', row.sku)}
    {@render detailField('Import MPN', row.mpn)}
    {@render detailField('Import manufacturer', row.manufacturer)}
    {@render detailField('Import name', row.part_name)}
    {@render detailField('Import description', row.raw_description)}
    {@render detailField('Resolved package', row.resolved_part_package)}
    {#if interp?.params?.length}
      <div class="sm:col-span-2 lg:col-span-4">
        <div class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">Parsed parameters</div>
        <div class="mt-1 flex flex-wrap gap-1">
          {#each interp.params as p}
            <span class="rounded bg-secondary px-1.5 py-0.5 font-mono text-xs">
              {paramDefBySlug.get(p.slug)?.display_name ?? p.slug}: {p.raw}
              {#if p.value_si != null}<span class="text-muted-foreground">({p.value_si})</span>{/if}
            </span>
          {/each}
        </div>
      </div>
    {/if}
    {#if row.resolve_item?.error_message}
      <div class="sm:col-span-2 lg:col-span-4">
        <div class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">Resolve error</div>
        <div class="mt-1 font-mono text-xs text-danger">{row.resolve_item.error_message}</div>
      </div>
    {/if}
  </div>
{/snippet}

{#snippet detailField(label: string, value: string | null | undefined)}
  <div>
    <div class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">{label}</div>
    <div class="mt-0.5 font-mono text-xs break-words {value ? '' : 'text-muted-foreground'}">
      {value || 'not set'}
    </div>
  </div>
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

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    getBuild, submitBuild, checkoutBuild, cancelBuild,
    getBuildPreview, setExcludedDesignators, setLineItemOverride,
    refreshBuildSubstituteSelections, patchBuildSubstituteSelections,
    type BuildPreviewItem,
  } from '$lib/api/builds.js';
  import { startKittingSession, getKittingSession, getKittingItems } from '$lib/api/buildKitting.js';
  import { scanSessionStore } from '$lib/stores/scanSession.svelte.js';
  import { ApiError } from '$lib/api/client.js';
  import { expandDesignators } from '$lib/utils/designators.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Dialog, Popover } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { AlertTriangle, CheckCircle, Loader2, RefreshCw, Send, XCircle } from 'lucide-svelte';
  import type { BuildStatus, BuildConsumedItem, BuildPartAllocation } from '$lib/types/orders.js';

  let { teamId, buildId }: { teamId: string; buildId: string } = $props();

  const qc = useQueryClient();

  let buildQuery = createQuery(() => ({
    queryKey: ['build', buildId],
    queryFn: () => getBuild(teamId, buildId),
  }));

  // Fetch preview for draft/pending/ready builds (parts table + checkout modal)
  let previewQuery = createQuery(() => ({
    queryKey: ['build-preview', buildId],
    queryFn: () => getBuildPreview(teamId, buildId),
    enabled: !!buildQuery.data && ['draft', 'pending', 'ready'].includes(buildQuery.data.status),
  }));

  // ── Excluded designators (optimistic local state) ──────────────────────────
  let pendingExcluded = $state<Set<string> | null>(null);
  const excludedDesignators = $derived(
    pendingExcluded ?? new Set<string>(buildQuery.data?.excluded_designators ?? [])
  );

  async function applyExcluded(newSet: Set<string>) {
    pendingExcluded = newSet;
    try {
      await setExcludedDesignators(teamId, buildId, [...newSet]);
      qc.invalidateQueries({ queryKey: ['build-preview', buildId] });
      qc.invalidateQueries({ queryKey: ['build', buildId] });
    } catch (err: any) {
      pushToast('error', 'Failed to update excluded designators', err.message);
      pendingExcluded = null;
    }
  }

  function toggleDesignator(d: string) {
    const next = new Set(excludedDesignators);
    if (next.has(d)) next.delete(d); else next.add(d);
    applyExcluded(next);
  }

  function toggleAllDesignators(designators: string[], include: boolean) {
    const next = new Set(excludedDesignators);
    for (const d of designators) { if (include) next.delete(d); else next.add(d); }
    applyExcluded(next);
  }

  function invertDesignators(designators: string[]) {
    const next = new Set(excludedDesignators);
    for (const d of designators) { if (next.has(d)) next.delete(d); else next.add(d); }
    applyExcluded(next);
  }

  // ── Line-item overrides ─────────────────────────────────────────────────────
  let settingOverride = $state<Record<string, boolean>>({});

  async function setOverride(row: BuildPreviewItem, override: 'use_available' | 'skip' | null) {
    settingOverride[row.bom_line_item_id] = true;
    try {
      await setLineItemOverride(teamId, buildId, row.bom_line_item_id, override);
      qc.invalidateQueries({ queryKey: ['build-preview', buildId] });
      qc.invalidateQueries({ queryKey: ['build', buildId] });
    } catch (err: any) {
      pushToast('error', 'Failed to update override', err.message);
    } finally {
      settingOverride[row.bom_line_item_id] = false;
    }
  }

  // ── Paste exclusions popover ────────────────────────────────────────────────
  let pasteOpen = $state(false);
  let pasteText = $state('');

  function parsePasteList(text: string): string[] {
    return text.split(/[\s,;]+/).map(s => s.trim()).filter(Boolean);
  }

  async function applyPasteExclusions(parsed: string[]) {
    const allItemDesignators = new Set(
      (previewQuery.data?.items ?? []).flatMap(it => expandDesignators(it.reference_designators ?? ''))
    );
    const valid = parsed.filter(d => allItemDesignators.has(d));
    const next = new Set([...excludedDesignators, ...valid]);
    pasteOpen = false;
    pasteText = '';
    await applyExcluded(next);
  }

  // ── Substitute selection state ──────────────────────────────────────────────
  let refreshingSelections = $state(false);
  let patchingSelection = $state<Record<string, boolean>>({});

  let selectionsReady = $derived(
    !!buildQuery.data?.selections_checked_at && !buildQuery.data?.selections_stale
  );

  async function handleRefreshSelections() {
    refreshingSelections = true;
    try {
      await refreshBuildSubstituteSelections(teamId, buildId);
      qc.invalidateQueries({ queryKey: ['build', buildId] });
      pushToast('info', 'Recalculating substitute selections…');
    } catch (err: any) {
      pushToast('error', 'Failed to refresh selections', err.message);
    } finally {
      refreshingSelections = false;
    }
  }

  async function handleChangeSelection(lineItemId: string, partId: string | null) {
    patchingSelection[lineItemId] = true;
    try {
      const selections = partId ? [{ bom_line_item_id: lineItemId, part_id: partId }] : [];
      await patchBuildSubstituteSelections(teamId, buildId, selections);
      qc.invalidateQueries({ queryKey: ['build', buildId] });
    } catch (err: any) {
      pushToast('error', 'Failed to update selection', err.message);
    } finally {
      patchingSelection[lineItemId] = false;
    }
  }

  // ── Action states ───────────────────────────────────────────────────────────
  let submitting = $state(false);
  let cancelOpen = $state(false);

  // ── Checkout modal ──────────────────────────────────────────────────────────
  let checkoutOpen = $state(false);
  let checkoutAssembledQty = $state(1);
  let checkoutSubmitting = $state(false);

  async function handleSubmit() {
    submitting = true;
    try {
      await submitBuild(teamId, buildId);
      qc.invalidateQueries({ queryKey: ['build', buildId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      pushToast('success', 'Build submitted for fulfilment');
    } catch (err: any) {
      pushToast('error', 'Failed to submit build', err.message);
    } finally {
      submitting = false;
    }
  }

  async function handleCancel() {
    try {
      await cancelBuild(teamId, buildId);
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      pushToast('success', 'Build cancelled');
      navigate(`/teams/${teamId}/builds`);
    } catch (err: any) {
      pushToast('error', 'Failed to cancel build', err.message);
    }
  }

  function openCheckout() {
    checkoutAssembledQty = buildQuery.data?.quantity ?? 1;
    checkoutOpen = true;
  }

  async function handleCheckout() {
    checkoutSubmitting = true;
    try {
      const buildQty = buildQuery.data?.quantity ?? checkoutAssembledQty;
      const body: { assembled_quantity?: number } = {};
      if (checkoutAssembledQty < buildQty) body.assembled_quantity = checkoutAssembledQty;
      await checkoutBuild(teamId, buildId, body);
      qc.invalidateQueries({ queryKey: ['build', buildId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      checkoutOpen = false;
      pushToast('success', 'Build checked out — assembly recorded');
    } catch (err: any) {
      if (err instanceof ApiError && err.status === 409 && err.code === 'INSUFFICIENT_STOCK') {
        pushToast('error', 'Insufficient stock for some items. Use per-item overrides before checking out.');
      } else {
        pushToast('error', 'Failed to checkout build', err.message);
      }
    } finally {
      checkoutSubmitting = false;
    }
  }

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
      case 'ready': return 'Ready to Assemble';
      case 'assembled': return 'Assembled';
      case 'cancelled': return 'Cancelled';
      default: return status;
    }
  }

  // Ready for checkout: every item is sufficient, overridden, or fully excluded
  let allReadyForCheckout = $derived(
    (previewQuery.data?.items ?? []).length > 0 &&
    (previewQuery.data?.items ?? []).every(pi =>
      pi.sufficient || pi.override !== null || pi.effective_total_quantity <= 0
    )
  );

  const canEditBuild = $derived(
    buildQuery.data?.status === 'draft' || buildQuery.data?.status === 'pending'
  );

  // ── Kitting ─────────────────────────────────────────────────────────────────

  const scanSession = $derived(scanSessionStore.current);
  const canSendToScanner = $derived(
    !!scanSession?.liveConnected &&
    ['pending', 'ordered', 'ready'].includes(buildQuery.data?.status ?? '')
  );

  let activeKittingSessionId = $state<string | null>(null);
  let kittingLoading = $state(false);

  const kittingSessionQuery = createQuery(() => ({
    queryKey: ['kitting', teamId, activeKittingSessionId],
    queryFn: () => getKittingSession(teamId, activeKittingSessionId!),
    enabled: !!activeKittingSessionId,
    refetchInterval: false,
  }));

  const kittingItemsQuery = createQuery(() => ({
    queryKey: ['kitting', teamId, activeKittingSessionId, 'items'],
    queryFn: () => getKittingItems(teamId, activeKittingSessionId!),
    enabled: !!activeKittingSessionId,
    refetchInterval: false,
  }));

  async function handleSendToScanner() {
    if (!scanSession || kittingLoading) return;
    kittingLoading = true;
    try {
      const res = await startKittingSession(teamId, scanSession.id, buildId);
      activeKittingSessionId = res.kitting_session_id;
      pushToast('success', 'Kitting session started on scanner');
    } catch (err: any) {
      pushToast('error', 'Failed to start kitting', err.message);
    } finally {
      kittingLoading = false;
    }
  }

  // DataTable sources
  const consumedColumns: ColumnSpec<BuildConsumedItem>[] = [
    { key: 'part_mpn', header: 'Part MPN', type: 'text', sortable: true },
    { key: 'quantity_deducted', header: 'Qty Deducted', type: 'numeric', align: 'right', sortable: true },
  ];
  const consumedSource = new LocalDataSource<BuildConsumedItem>(
    () => buildQuery.data?.items ?? [],
    consumedColumns,
  );

  const allocColumns: ColumnSpec<BuildPartAllocation>[] = [
    { key: 'part_mpn', header: 'Part MPN', type: 'text', sortable: true },
    { key: 'quantity_needed', header: 'Needed', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_reserved', header: 'Reserved', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_received', header: 'Received', type: 'numeric', align: 'right', sortable: true },
    { key: '_status', header: 'Status', type: 'custom' },
  ];
  const allocSource = new LocalDataSource<BuildPartAllocation>(
    () => buildQuery.data?.allocations ?? [],
    allocColumns,
  );

  const previewColumns: ColumnSpec<BuildPreviewItem>[] = [
    { key: 'resolved_part_mpn', header: 'Part MPN', type: 'text', sortable: true },
    { key: 'reference_designators', header: 'Ref Des', type: 'custom' },
    { key: 'effective_quantity_per_board', header: 'Qty/Board', type: 'numeric', align: 'right', sortable: true },
    { key: 'effective_total_quantity', header: 'Total Needed', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_on_hand', header: 'On Hand', type: 'numeric', align: 'right', sortable: true },
    { key: 'sufficient', header: 'Stock', type: 'custom' },
    { key: 'substitutes', header: 'Using', type: 'custom' },
  ];
  function getPreviewValue(row: BuildPreviewItem, key: string): unknown {
    if (key === 'sufficient') {
      // Sort rank: Short=0, Partial=1, Skipped=2, Excluded=3, In Stock=4
      if (row.override === 'skip') return 2;
      if (row.override === 'use_available') return 1;
      if (row.effective_total_quantity <= 0) return 3;
      return row.sufficient ? 4 : 0;
    }
    return (row as unknown as Record<string, unknown>)[key];
  }

  const previewSource = new LocalDataSource<BuildPreviewItem>(
    () => previewQuery.data?.items ?? [],
    previewColumns,
    getPreviewValue,
  );
</script>

{#snippet renderMpn(value: unknown, row: BuildConsumedItem | BuildPartAllocation)}
    <span class="font-mono font-medium text-xs hover:underline">{value && row.part_id ? value : "—"}</span>
{/snippet}

{#snippet renderPreviewMpn(value: unknown, row: BuildPreviewItem)}
    <span class="font-mono font-medium text-xs hover:underline">{value && row.resolved_part_id ? value : "—"}</span>
{/snippet}

{#snippet renderAllocStatus(_value: unknown, row: BuildPartAllocation)}
  {@const covered = row.quantity_reserved + row.quantity_received >= row.quantity_needed}
  {@const partial = !covered && (row.quantity_reserved + row.quantity_received) > 0}
  {#if covered}
    <Badge variant="success">Covered</Badge>
  {:else if partial}
    <Badge variant="warning">Partial</Badge>
  {:else}
    <Badge variant="destructive">Missing</Badge>
  {/if}
{/snippet}

{#snippet renderSufficient(value: unknown, row: BuildPreviewItem)}
  {#if row.override === 'skip'}
    <Badge variant="secondary">Skipped</Badge>
  {:else if row.override === 'use_available'}
    <Badge variant="warning">Partial</Badge>
  {:else if row.effective_total_quantity <= 0}
    <Badge variant="secondary">Excluded</Badge>
  {:else}
    <Badge variant={value ? 'success' : 'destructive'}>{value ? 'In Stock' : 'Short'}</Badge>
  {/if}
{/snippet}

{#snippet renderRefDes(value: unknown, row: BuildPreviewItem)}
  <span class="font-mono text-xs">
    {value as string}
    {#if row.excluded_designator_count > 0}
      <span class="text-muted-foreground ml-1">({row.excluded_designator_count} excluded)</span>
    {/if}
  </span>
{/snippet}

{#snippet renderSubstituteSelection(value: unknown, row: BuildPreviewItem)}
  {@const subs = value as BuildPreviewItem['substitutes']}
  {@const currentSelection = buildQuery.data?.substitute_selections?.find(
    s => s.bom_line_item_id === row.bom_line_item_id
  )}
  {#if !subs || subs.length === 0}
    <span class="text-xs text-muted-foreground">—</span>
  {:else}
    <select
      class="h-7 rounded-md border border-input bg-background px-2 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
      disabled={patchingSelection[row.bom_line_item_id]}
      value={currentSelection?.selected_part_id ?? ''}
      onclick={(e) => e.stopPropagation()}
      onchange={(e) => {
        const val = (e.target as HTMLSelectElement).value;
        handleChangeSelection(row.bom_line_item_id, val || null);
      }}
    >
      <option value="">Primary</option>
      {#each subs as sub (sub.part_id)}
        <option value={sub.part_id}>{sub.part_mpn ?? sub.part_id} ({sub.quantity_on_hand} on hand)</option>
      {/each}
    </select>
  {/if}
{/snippet}

{#snippet previewActions(row: BuildPreviewItem)}
  {#if canEditBuild}
    {@const isShort = row.effective_total_quantity > row.quantity_on_hand}
    {#if isShort && row.override == null}
      {#if row.quantity_on_hand > 0}
        <Button
          size="sm" variant="outline" class="h-7 text-xs whitespace-nowrap"
          disabled={!!settingOverride[row.bom_line_item_id]}
          onclick={(e: MouseEvent) => { e.stopPropagation(); setOverride(row, 'use_available'); }}
        >Use {row.quantity_on_hand}</Button>
      {:else}
        <Button
          size="sm" variant="outline" class="h-7 text-xs"
          disabled={!!settingOverride[row.bom_line_item_id]}
          onclick={(e: MouseEvent) => { e.stopPropagation(); setOverride(row, 'skip'); }}
        >Skip</Button>
      {/if}
    {:else if row.override === 'use_available'}
      <div class="flex items-center gap-1">
        <span class="text-muted-foreground text-xs whitespace-nowrap">using {row.available_quantity}</span>
        <Button
          size="sm" variant="ghost" class="h-6 text-xs px-1"
          disabled={!!settingOverride[row.bom_line_item_id]}
          onclick={(e: MouseEvent) => { e.stopPropagation(); setOverride(row, null); }}
        >undo</Button>
      </div>
    {:else if row.override === 'skip'}
      <div class="flex items-center gap-1">
        <span class="text-muted-foreground text-xs">skipped</span>
        <Button
          size="sm" variant="ghost" class="h-6 text-xs px-1"
          disabled={!!settingOverride[row.bom_line_item_id]}
          onclick={(e: MouseEvent) => { e.stopPropagation(); setOverride(row, null); }}
        >undo</Button>
      </div>
    {/if}
  {/if}
{/snippet}

{#snippet previewDetailPanel(row: BuildPreviewItem)}
  {@const designators = expandDesignators(row.reference_designators ?? '')}
  {@const total = designators.length}
  {@const includedCount = designators.filter(d => !excludedDesignators.has(d)).length}
  <div class="px-4 py-3 bg-muted/20 space-y-3">
    <div class="flex items-center justify-between gap-4">
      <div class="flex items-center gap-1">
        <Button size="sm" variant="ghost" class="h-6 text-xs px-2"
          onclick={() => toggleAllDesignators(designators, true)}>All</Button>
        <Button size="sm" variant="ghost" class="h-6 text-xs px-2"
          onclick={() => toggleAllDesignators(designators, false)}>None</Button>
        <Button size="sm" variant="ghost" class="h-6 text-xs px-2"
          onclick={() => invertDesignators(designators)}>Invert</Button>
      </div>
      <span class="text-xs text-muted-foreground">{includedCount} / {total} included</span>
    </div>
    <div class="flex flex-wrap gap-1">
      {#each designators as d (d)}
        {@const isExcluded = excludedDesignators.has(d)}
        <button
          class="px-2 py-0.5 rounded text-xs border transition-colors
            {isExcluded
              ? 'border-muted-foreground/30 bg-muted text-muted-foreground line-through opacity-60'
              : 'border-border bg-background text-foreground hover:bg-accent'}"
          disabled={!canEditBuild}
          onclick={() => toggleDesignator(d)}
        >{d}</button>
      {/each}
    </div>
    {#if canEditBuild && row.effective_total_quantity > row.quantity_on_hand && row.override == null}
      <div class="flex items-center justify-between rounded-md bg-[#fffbeb] border border-[#fde68a] px-3 py-2 text-xs text-[black]">
        <span class="flex items-center gap-1.5">
          <AlertTriangle class="h-3.5 w-3.5 shrink-0" />
          Still short {row.effective_total_quantity - row.quantity_on_hand} after exclusions
        </span>
        {#if row.quantity_on_hand > 0}
          <Button size="sm" variant="outline" class="h-6 text-xs"
            onclick={() => setOverride(row, 'use_available')}
          >Use {row.quantity_on_hand} available</Button>
        {:else}
          <Button size="sm" variant="outline" class="h-6 text-xs"
            onclick={() => setOverride(row, 'skip')}
          >Skip this part</Button>
        {/if}
      </div>
    {/if}
  </div>
{/snippet}

{#snippet pasteToolbar()}
  {#if canEditBuild}
    {@const parsed = parsePasteList(pasteText)}
    <Popover.Root open={pasteOpen} onOpenChange={(v) => { pasteOpen = v; if (!v) pasteText = ''; }}>
      <Popover.Trigger>
        <Button size="sm" variant="outline" class="h-7 text-xs">Exclude designators…</Button>
      </Popover.Trigger>
      <Popover.Content class="z-50 w-80 rounded-md border border-border bg-popover p-4 shadow-md space-y-3">
        <p class="text-sm font-medium">Paste DNP list</p>
        <p class="text-xs text-muted-foreground">Paste comma, space, or newline-separated designators to exclude (e.g. from a KiCad/Altium DNP export).</p>
        <textarea
          bind:value={pasteText}
          placeholder={"R1, C5, U4\nR3 C10"}
          class="w-full min-h-20 rounded-md border border-input bg-background px-3 py-2 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring resize-y"
        ></textarea>
        {#if parsed.length > 0}
          <p class="text-xs text-muted-foreground">{parsed.length} designator{parsed.length !== 1 ? 's' : ''} parsed</p>
        {/if}
        <div class="flex justify-end gap-2">
          <Button variant="outline" size="sm" onclick={() => { pasteOpen = false; pasteText = ''; }}>Cancel</Button>
          <Button size="sm" disabled={parsed.length === 0} onclick={() => applyPasteExclusions(parsed)}>Apply</Button>
        </div>
      </Popover.Content>
    </Popover.Root>
  {/if}
{/snippet}

<div class="flex flex-col h-full">
  {#if buildQuery.isPending}
    <Topbar title="Build" breadcrumbs={[{ label: 'Builds', href: `/teams/${teamId}/builds` }]} />
    <div class="p-6 text-sm text-muted-foreground">Loading build…</div>
  {:else if buildQuery.isError}
    <Topbar title="Build" breadcrumbs={[{ label: 'Builds', href: `/teams/${teamId}/builds` }]} />
    <div class="p-6 text-sm text-destructive">Error: {buildQuery.error.message}</div>
  {:else if buildQuery.data}
    {@const build = buildQuery.data}
    <Topbar
      title={build.project_name}
      breadcrumbs={[{ label: 'Builds', href: `/teams/${teamId}/builds` }]}
    >
      {#snippet badges()}
        <Badge variant={statusVariant(build.status)}>{statusLabel(build.status)}</Badge>
      {/snippet}
      {#snippet actions()}
        {#if canSendToScanner && !activeKittingSessionId}
          <Button
            size="sm"
            variant="outline"
            onclick={handleSendToScanner}
            disabled={kittingLoading}
          >
            {kittingLoading ? 'Starting…' : 'Send to Scanner'}
          </Button>
        {/if}
        {#if build.status === 'draft'}
          {#if previewQuery.data && allReadyForCheckout}
            <Button size="sm" onclick={openCheckout}>
              <CheckCircle class="h-4 w-4 mr-1" /> Checkout
            </Button>
          {/if}
          <Button
            variant={previewQuery.data && allReadyForCheckout ? 'outline' : 'default'}
            size="sm"
            onclick={handleSubmit}
            disabled={submitting}
          >
            <Send class="h-4 w-4 mr-1" />
            {submitting ? 'Submitting…' : 'Submit for Fulfilment'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            class="text-destructive border-destructive hover:bg-destructive/10"
            onclick={() => { cancelOpen = true; }}
          >
            <XCircle class="h-4 w-4 mr-1" /> Cancel Build
          </Button>
        {:else if build.status === 'pending'}
          <Button
            variant="outline"
            size="sm"
            class="text-destructive border-destructive hover:bg-destructive/10"
            onclick={() => { cancelOpen = true; }}
          >
            <XCircle class="h-4 w-4 mr-1" /> Cancel Build
          </Button>
        {:else if build.status === 'ordered'}
          <Button
            variant="outline"
            size="sm"
            class="text-destructive border-destructive hover:bg-destructive/10"
            onclick={() => { cancelOpen = true; }}
          >
            <XCircle class="h-4 w-4 mr-1" /> Cancel Build
          </Button>
        {:else if build.status === 'ready'}
          <Button size="sm" onclick={openCheckout}>
            <CheckCircle class="h-4 w-4 mr-1" /> Checkout
          </Button>
          <Button
            variant="outline"
            size="sm"
            class="text-destructive border-destructive hover:bg-destructive/10"
            onclick={() => { cancelOpen = true; }}
          >
            <XCircle class="h-4 w-4 mr-1" /> Cancel Build
          </Button>
        {/if}
      {/snippet}
    </Topbar>

    <div class="flex-1 overflow-auto p-6 space-y-6">
      <!-- Metadata card -->
      <MetadataCard title={build.project_name} description={build.notes ?? undefined}>
        {#snippet badges()}
          <Badge variant={statusVariant(build.status)}>{statusLabel(build.status)}</Badge>
        {/snippet}
        <div class="grid grid-cols-2 gap-x-8 gap-y-2 text-sm sm:grid-cols-4">
          <div class="flex flex-col gap-0.5">
            <span class="text-xs text-muted-foreground uppercase tracking-wide">Project</span>
            <a
              href="/teams/{teamId}/projects/{build.project_id}"
              class="font-medium hover:underline"
              onclick={(e) => { e.preventDefault(); navigate(`/teams/${teamId}/projects/${build.project_id}`); }}
            >{build.project_name}</a>
          </div>
          <div class="flex flex-col gap-0.5">
            <span class="text-xs text-muted-foreground uppercase tracking-wide">Revision</span>
            <span class="font-medium">{build.bom_revision_label}</span>
          </div>
          <div class="flex flex-col gap-0.5">
            <span class="text-xs text-muted-foreground uppercase tracking-wide">Planned qty</span>
            <span class="font-medium tabular-nums">{build.quantity}</span>
          </div>
          {#if build.status === 'assembled'}
            <div class="flex flex-col gap-0.5">
              <span class="text-xs text-muted-foreground uppercase tracking-wide">Assembled qty</span>
              <span class="font-medium tabular-nums {(build.assembled_quantity ?? build.quantity) < build.quantity ? 'text-yellow-600 dark:text-yellow-400' : ''}">
                {build.assembled_quantity ?? build.quantity}
                {#if (build.assembled_quantity ?? build.quantity) < build.quantity}
                  <span class="text-xs text-muted-foreground">(of {build.quantity})</span>
                {/if}
              </span>
            </div>
          {/if}
          <div class="flex flex-col gap-0.5">
            <span class="text-xs text-muted-foreground uppercase tracking-wide">Created</span>
            <span class="font-medium"><DateDisplay value={build.created_at} /></span>
          </div>
          {#if build.created_by_display_name}
            <div class="flex flex-col gap-0.5">
              <span class="text-xs text-muted-foreground uppercase tracking-wide">Created by</span>
              <span class="font-medium">{build.created_by_display_name}</span>
            </div>
          {/if}
          {#if build.assembled_at}
            <div class="flex flex-col gap-0.5">
              <span class="text-xs text-muted-foreground uppercase tracking-wide">Assembled</span>
              <span class="font-medium"><DateDisplay value={build.assembled_at} /></span>
            </div>
          {/if}
          {#if build.assembled_by_display_name}
            <div class="flex flex-col gap-0.5">
              <span class="text-xs text-muted-foreground uppercase tracking-wide">Assembled by</span>
              <span class="font-medium">{build.assembled_by_display_name}</span>
            </div>
          {/if}
        </div>
      </MetadataCard>

      <!-- Kitting progress -->
      {#if activeKittingSessionId}
        {@const ks = kittingSessionQuery.data}
        {@const ki = kittingItemsQuery.data?.items ?? []}
        {@const prog = ks?.progress}
        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Kitting in Progress</h3>
            {#if prog}
              <span class="text-xs text-muted-foreground">
                {prog.confirmed + prog.shortfall + prog.skipped} / {prog.pending + prog.confirmed + prog.shortfall + prog.skipped} done
              </span>
            {/if}
          </div>

          {#if prog && (prog.pending + prog.confirmed + prog.shortfall + prog.skipped) > 0}
            {@const total = prog.pending + prog.confirmed + prog.shortfall + prog.skipped}
            {@const done = prog.confirmed + prog.shortfall + prog.skipped}
            <div class="h-1.5 rounded-full bg-muted overflow-hidden">
              <div class="h-full bg-emerald-500 transition-all" style="width: {Math.round((done / total) * 100)}%"></div>
            </div>
            <div class="flex gap-4 text-xs">
              {#if prog.confirmed > 0}<span class="text-emerald-600 dark:text-emerald-400">✓ {prog.confirmed} confirmed</span>{/if}
              {#if prog.shortfall > 0}<span class="text-amber-600 dark:text-amber-400">⚠ {prog.shortfall} shortfall</span>{/if}
              {#if prog.pending > 0}<span class="text-muted-foreground">· {prog.pending} pending</span>{/if}
            </div>
          {/if}

          {#if ki.length > 0}
            <div class="space-y-1 max-h-48 overflow-y-auto">
              {#each [...ki].sort((a, b) => {
                const order: Record<string, number> = { pending: 0, shortfall: 1, confirmed: 2, skipped: 3 };
                return (order[a.status] ?? 0) - (order[b.status] ?? 0);
              }) as item (item.part_id)}
                <div class="flex items-center gap-2 py-1 px-2 rounded text-xs
                  {item.status === 'confirmed' ? 'bg-emerald-500/10' : item.status === 'shortfall' ? 'bg-amber-500/10' : ''}">
                  <span class="shrink-0 w-4 text-center
                    {item.status === 'confirmed' ? 'text-emerald-500' : item.status === 'shortfall' ? 'text-amber-500' : 'text-muted-foreground'}">
                    {item.status === 'confirmed' ? '✓' : item.status === 'shortfall' ? '⚠' : '·'}
                  </span>
                  <span class="flex-1 font-mono truncate">{item.part_mpn ?? item.part_id}</span>
                  <span class="shrink-0 text-muted-foreground tabular-nums">{item.quantity_confirmed}/{item.quantity_needed}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      <!-- Substitute selections staleness banner (draft builds only) -->
      {#if build.status === 'draft'}
        {#if !build.selections_checked_at}
          <div class="flex items-center gap-3 rounded-md border border-blue-200 bg-blue-50 px-4 py-3 text-sm dark:border-blue-800 dark:bg-blue-950/30">
            <Loader2 class="h-4 w-4 shrink-0 animate-spin text-blue-500" />
            <span class="text-blue-700 dark:text-blue-400">Calculating substitute selections…</span>
          </div>
        {:else if build.selections_stale}
          <div class="flex items-center justify-between gap-3 rounded-md border border-yellow-200 bg-yellow-50 px-4 py-3 text-sm dark:border-yellow-800 dark:bg-yellow-950/30">
            <div class="flex items-center gap-2">
              <AlertTriangle class="h-4 w-4 shrink-0 text-yellow-500" />
              <span class="text-yellow-700 dark:text-yellow-400">Substitute selections may be out of date — inventory has changed since last check.</span>
            </div>
            <Button
              variant="outline" size="sm" disabled={refreshingSelections}
              onclick={handleRefreshSelections} class="shrink-0"
            >
              <RefreshCw class="h-3.5 w-3.5 mr-1 {refreshingSelections ? 'animate-spin' : ''}" />
              Refresh
            </Button>
          </div>
        {/if}
      {/if}

      <!-- Parts section -->
      <section class="space-y-2">
        {#if build.status === 'assembled'}
          <DataTable
            source={consumedSource}
            getRowKey={(row) => row.inventory_entry_id}
            renderers={{ part_mpn: renderMpn }}
            emptyMessage="No consumed parts recorded."
            onRowClick={(row) => { navigate(`/teams/${teamId}/parts/${row.part_id}`); }}
            title="Consumed Parts"
          />

        {:else if build.status === 'ordered' || build.status === 'ready'}
          <DataTable
            source={allocSource}
            getRowKey={(row) => row.part_id}
            renderers={{ part_mpn: renderMpn, _status: renderAllocStatus }}
            emptyMessage="No allocation data available."
            onRowClick={(row) => { navigate(`/teams/${teamId}/parts/${row.part_id}`); }}
            title="Ordered Parts"
          />

        {:else}
          <!-- Preview table for draft/pending builds -->
          {#if previewQuery.isPending}
            <div class="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 class="h-4 w-4 animate-spin" /> Loading parts…
            </div>
          {:else if previewQuery.isError}
            <div class="text-sm text-destructive">Failed to load parts preview.</div>
          {:else if previewQuery.data}
            <DataTable
              source={previewSource}
              getRowKey={(row) => row.bom_line_item_id}
              renderers={{
                resolved_part_mpn: renderPreviewMpn,
                reference_designators: renderRefDes,
                sufficient: renderSufficient,
                substitutes: renderSubstituteSelection,
              }}
              detailPanel={previewDetailPanel}
              actions={previewActions}
              toolbarLeft={pasteToolbar}
              emptyMessage="No parts data."
              title="Required Parts"
            />
            {#if build.status === 'pending'}
              <p class="text-sm text-muted-foreground mt-1">
                This build is pending fulfilment. Shortfalls will be covered via the shopping cart.
              </p>
            {/if}
          {/if}
        {/if}
      </section>

      <!-- Linked Orders section -->
      {#if build.linked_order_ids && build.linked_order_ids.length > 0}
        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Linked Orders</h3>
          <div class="flex flex-wrap gap-2">
            {#each build.linked_order_ids as orderId (orderId)}
              <Button
                variant="outline" size="sm"
                onclick={() => navigate(`/teams/${teamId}/orders/${orderId}`)}
              >
                Order {orderId.slice(0, 8)}…
              </Button>
            {/each}
          </div>
        </section>
      {/if}
    </div>
  {/if}
</div>

<!-- Cancel confirm dialog -->
<ConfirmDialog
  bind:open={cancelOpen}
  title="Cancel Build"
  description="This will release any soft reservations for this build. Linked orders will not be cancelled — cancel them separately if needed."
  confirmLabel="Cancel Build"
  destructive
  onconfirm={handleCancel}
/>

<!-- Checkout modal -->
<Dialog.Root bind:open={checkoutOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex w-full max-w-2xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
      style="max-height: 85vh;"
    >
      <div class="shrink-0 p-6 pb-4 border-b border-border">
        <Dialog.Title class="text-lg font-semibold">Checkout Build</Dialog.Title>
        <p class="text-sm text-muted-foreground mt-1">
          Review parts to be deducted from inventory.
        </p>
        <div class="mt-4 flex items-end gap-4">
          <div class="w-36 space-y-1">
            <Label for="assembled-qty">Assembled quantity</Label>
            <Input
              id="assembled-qty"
              type="number"
              bind:value={checkoutAssembledQty}
              min="1"
              max={buildQuery.data?.quantity ?? 1}
            />
          </div>
          {#if buildQuery.data && checkoutAssembledQty < buildQuery.data.quantity && checkoutAssembledQty >= 1}
            <p class="text-sm text-muted-foreground pb-0.5">
              Remaining {buildQuery.data.quantity - checkoutAssembledQty} board(s) will stay in the build pipeline.
            </p>
          {/if}
        </div>
      </div>

      <div class="min-h-0 flex-1 overflow-auto px-6 py-4">
        {#if previewQuery.isFetching && !previewQuery.data}
          <div class="flex items-center gap-2 text-sm text-muted-foreground py-8 justify-center">
            <Loader2 class="h-4 w-4 animate-spin" /> Loading preview…
          </div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Part MPN</TableHead>
                <TableHead class="text-right">Per Board</TableHead>
                <TableHead class="text-right">Total</TableHead>
                <TableHead class="text-right">On Hand</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each previewQuery.data?.items ?? [] as pi (pi.bom_line_item_id)}
                <TableRow>
                  <TableCell class="font-mono text-xs">{pi.resolved_part_mpn ?? '—'}</TableCell>
                  <TableCell class="text-right tabular-nums">{pi.effective_quantity_per_board}</TableCell>
                  <TableCell class="text-right tabular-nums">{pi.effective_total_quantity}</TableCell>
                  <TableCell class="text-right tabular-nums">{pi.quantity_on_hand}</TableCell>
                  <TableCell>
                    {#if pi.override === 'skip'}
                      <Badge variant="secondary">Skipped</Badge>
                    {:else if pi.override === 'use_available'}
                      <Badge variant="warning">Partial ({pi.available_quantity})</Badge>
                    {:else if pi.effective_total_quantity <= 0}
                      <Badge variant="secondary">Excluded</Badge>
                    {:else}
                      <Badge variant={pi.sufficient ? 'success' : 'destructive'}>
                        {pi.sufficient ? 'OK' : 'Short'}
                      </Badge>
                    {/if}
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        {/if}
      </div>

      <div class="shrink-0 p-6 pt-4 flex justify-end gap-2 border-t border-border">
        <Dialog.Close>
          <Button variant="outline" type="button">Cancel</Button>
        </Dialog.Close>
        <Button
          onclick={handleCheckout}
          disabled={checkoutSubmitting || checkoutAssembledQty < 1}
        >
          {checkoutSubmitting ? 'Checking out…' : 'Confirm Checkout'}
        </Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

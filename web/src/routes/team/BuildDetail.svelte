<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    getBuild, submitBuild, checkoutBuild, cancelBuild,
    previewBuild, type BuildPreviewItem,
  } from '$lib/api/builds.js';
  import { ApiError } from '$lib/api/client.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { CheckCircle, Loader2, Send, XCircle } from 'lucide-svelte';
  import type { BuildStatus, BuildConsumedItem, BuildPartAllocation } from '$lib/types/orders.js';

  let { teamId, buildId }: { teamId: string; buildId: string } = $props();

  const qc = useQueryClient();

  let buildQuery = createQuery(() => ({
    queryKey: ['build', buildId],
    queryFn: () => getBuild(teamId, buildId),
  }));

  // Fetch preview for draft/pending builds (parts table + checkout eligibility)
  let previewQuery = createQuery(() => ({
    queryKey: ['build-preview', buildQuery.data?.bom_revision_id, buildQuery.data?.quantity],
    queryFn: () => previewBuild(teamId, {
      bom_revision_id: buildQuery.data!.bom_revision_id,
      quantity: buildQuery.data!.quantity,
    }),
    enabled: !!buildQuery.data && (buildQuery.data.status === 'draft' || buildQuery.data.status === 'pending'),
  }));

  // Action states
  let submitting = $state(false);
  let cancelOpen = $state(false);

  // Checkout modal
  let checkoutOpen = $state(false);
  let checkoutItems = $state<BuildPreviewItem[]>([]);
  let checkoutPreviewing = $state(false);
  let checkoutAllowReconciliation = $state(false);
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

  async function openCheckout() {
    checkoutAssembledQty = buildQuery.data?.quantity ?? 1;
    checkoutAllowReconciliation = false;
    // Reuse preview data if already fetched (draft/pending)
    if (previewQuery.data) {
      checkoutItems = previewQuery.data.items;
      checkoutOpen = true;
      return;
    }
    // For ready state, fetch on demand
    checkoutPreviewing = true;
    checkoutOpen = true;
    try {
      const build = buildQuery.data!;
      const result = await previewBuild(teamId, {
        bom_revision_id: build.bom_revision_id,
        quantity: build.quantity,
      });
      checkoutItems = result.items;
    } catch (err: any) {
      pushToast('error', 'Failed to load checkout preview', err.message);
      checkoutOpen = false;
    } finally {
      checkoutPreviewing = false;
    }
  }

  async function handleCheckout() {
    checkoutSubmitting = true;
    try {
      const buildQty = buildQuery.data?.quantity ?? checkoutAssembledQty;
      const body: {
        assembled_quantity?: number;
        allow_stock_reconciliation?: boolean;
      } = {};
      if (checkoutAssembledQty < buildQty) {
        body.assembled_quantity = checkoutAssembledQty;
      }
      if (checkoutAllowReconciliation) {
        body.allow_stock_reconciliation = true;
      }
      await checkoutBuild(teamId, buildId, body);
      qc.invalidateQueries({ queryKey: ['build', buildId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      checkoutOpen = false;
      pushToast('success', 'Build checked out — assembly recorded');
    } catch (err: any) {
      if (err instanceof ApiError && err.status === 409 && err.code === 'INSUFFICIENT_STOCK') {
        pushToast('error', 'Insufficient stock. Enable "Allow stock reconciliation" and try again.');
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

  let allSufficient = $derived(
    (previewQuery.data?.items ?? []).every(pi => pi.sufficient)
  );

  let hasShortCheckoutItems = $derived(
    checkoutItems.some(pi => !pi.sufficient)
  );

  // DataTable sources for parts sections
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
    { key: 'reference_designators', header: 'Ref Des', type: 'text' },
    { key: 'quantity_per_board', header: 'Qty/Board', type: 'numeric', align: 'right', sortable: true },
    { key: 'total_quantity', header: 'Total Needed', type: 'numeric', align: 'right', sortable: true },
    { key: 'quantity_on_hand', header: 'On Hand', type: 'numeric', align: 'right', sortable: true },
    { key: 'sufficient', header: 'Stock', type: 'custom' },
  ];
  const previewSource = new LocalDataSource<BuildPreviewItem>(
    () => previewQuery.data?.items ?? [],
    previewColumns,
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

{#snippet renderSufficient(value: unknown, _row: unknown)}
  <Badge variant={value ? 'success' : 'destructive'}>
    {value ? 'In Stock' : 'Short'}
  </Badge>
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
        {#if build.status === 'draft'}
          {#if previewQuery.data && allSufficient}
            <Button size="sm" onclick={openCheckout}>
              <CheckCircle class="h-4 w-4 mr-1" /> Checkout
            </Button>
          {/if}
          <Button
            variant={previewQuery.data && allSufficient ? 'outline' : 'default'}
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
          <Button size="sm" onclick={openCheckout} disabled={checkoutPreviewing}>
            <CheckCircle class="h-4 w-4 mr-1" />
            {checkoutPreviewing ? 'Loading…' : 'Checkout'}
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

      <!-- Parts section -->
      <section class="space-y-2">
        {#if build.status === 'assembled'}
          <!-- Deductions for assembled builds -->
          <DataTable
            source={consumedSource}
            getRowKey={(row) => row.inventory_entry_id}
            renderers={{ part_mpn: renderMpn }}
            emptyMessage="No consumed parts recorded."
            onRowClick={(row) => {navigate(`/teams/${teamId}/parts/${row.part_id}`);}}
            title="Consumed Parts"
          >
        </DataTable>

        {:else if build.status === 'ordered' || build.status === 'ready'}
          <!-- Allocation table for ordered/ready builds -->
          <DataTable
            source={allocSource}
            getRowKey={(row) => row.part_id}
            renderers={{ part_mpn: renderMpn, _status: renderAllocStatus }}
            emptyMessage="No allocation data available."
            onRowClick={(row) => {navigate(`/teams/${teamId}/parts/${row.part_id}`);}}
            title="Ordered Parts"
          >
        </DataTable>

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
              renderers={{ resolved_part_mpn: renderPreviewMpn, sufficient: renderSufficient }}
              emptyMessage="No parts data."
              onRowClick={(row) => {navigate(`/teams/${teamId}/parts/${row.resolved_part_id}`);}}
              title="Required Parts"
            >
            </DataTable>
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
                variant="outline"
                size="sm"
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
        {#if checkoutPreviewing}
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
                <TableHead>OK?</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each checkoutItems as pi (pi.bom_line_item_id)}
                <TableRow>
                  <TableCell class="font-mono text-xs">{pi.resolved_part_mpn ?? '—'}</TableCell>
                  <TableCell class="text-right tabular-nums">{pi.quantity_per_board}</TableCell>
                  <TableCell class="text-right tabular-nums">{pi.total_quantity}</TableCell>
                  <TableCell class="text-right tabular-nums">{pi.quantity_on_hand}</TableCell>
                  <TableCell>
                    <Badge variant={pi.sufficient ? 'success' : 'destructive'}>
                      {pi.sufficient ? 'OK' : 'Short'}
                    </Badge>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
          {#if hasShortCheckoutItems}
            <div class="mt-4 space-y-2 rounded-md border border-yellow-200 bg-yellow-50 p-3 dark:border-yellow-800 dark:bg-yellow-950/30">
              <p class="text-sm text-yellow-700 dark:text-yellow-400">
                Some parts have insufficient stock.
              </p>
              <label class="flex items-center gap-2 text-sm cursor-pointer">
                <input type="checkbox" bind:checked={checkoutAllowReconciliation} class="h-4 w-4" />
                Allow stock reconciliation (sets stock to 0 for short items before deducting)
              </label>
            </div>
          {/if}
        {/if}
      </div>

      <div class="shrink-0 p-6 pt-4 flex justify-end gap-2 border-t border-border">
        <Dialog.Close>
          <Button variant="outline" type="button">Cancel</Button>
        </Dialog.Close>
        <Button
          onclick={handleCheckout}
          disabled={checkoutSubmitting || checkoutPreviewing || checkoutAssembledQty < 1 || (hasShortCheckoutItems && !checkoutAllowReconciliation)}
        >
          {checkoutSubmitting ? 'Checking out…' : 'Confirm Checkout'}
        </Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

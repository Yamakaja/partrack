<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getOrder, markOrderPlaced, receiveLineItem, cancelOrder, updateOrderPurchaseOrderNumber, updateOrderNotes, getOrderLinkedBuilds } from '$lib/api/orders.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import DigiKeyCard from '$lib/components/orders/DigiKeyCard.svelte';
  import { DataTable, LocalDataSource, getNestedValue } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Pencil, Copy, Package, ScanLine } from 'lucide-svelte';
  import ScanModal from '$lib/components/scanner/ScanModal.svelte';
  import { getTrackingUrl } from '$lib/utils/tracking.js';
  import type { OrderStatus, OrderLineItem, DigiKeyStatusLineItem, BuildStatus } from '$lib/types/orders.js';

  /** Compare packaging strings, normalizing whitespace/parens for equivalence. */
  function packagingMatches(orderPkg: string | null, dkPack: string | undefined): boolean {
    if (!orderPkg || !dkPack) return true; // no data to compare
    const normalize = (s: string) => s.replace(/[\s()_-]/g, '').toLowerCase();
    return normalize(orderPkg).includes(normalize(dkPack)) || normalize(dkPack).includes(normalize(orderPkg));
  }

  /** Compare unit prices (string vs number) with tolerance for rounding. */
  function priceMatches(orderPrice: string, dkPrice: number | undefined): boolean {
    if (dkPrice === undefined) return true;
    return Math.abs(parseFloat(orderPrice) - dkPrice) < 0.0001;
  }

  let { teamId, orderId }: { teamId: string; orderId: string } = $props();

  const qc = useQueryClient();

  let orderQuery = createQuery(() => ({
    queryKey: ['order', orderId],
    queryFn: () => getOrder(teamId, orderId),
  }));

  let linkedBuildsQuery = createQuery(() => ({
    queryKey: ['order-builds', orderId],
    queryFn: () => getOrderLinkedBuilds(teamId, orderId),
  }));

  function buildStatusVariant(status: BuildStatus): 'secondary' | 'default' | 'warning' | 'success' | 'destructive' {
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

  function buildStatusLabel(status: BuildStatus): string {
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

  let receivingMode = $state(false);
  let receiveQtys = $state<Record<string, string>>({});
  let receivingItems = $state<Set<string>>(new Set());
  let overReceiveConfirm = $state<{ item: OrderLineItem; qty: number } | null>(null);
  let markingPlaced = $state(false);
  let placedRef = $state('');
  let showPlacedInput = $state(false);
  let showCancelConfirm = $state(false);
  let cancelling = $state(false);
  let editingPoNumber = $state(false);
  let poNumberDraft = $state('');
  let savingPoNumber = $state(false);
  let editingNotes = $state(false);
  let notesDraft = $state('');
  let savingNotes = $state(false);

  async function savePoNumber() {
    if (savingPoNumber) return;
    savingPoNumber = true;
    try {
      const value = poNumberDraft.trim() || null;
      await updateOrderPurchaseOrderNumber(teamId, orderId, value);
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      editingPoNumber = false;
    } catch (err: any) {
      pushToast('error', 'Failed to save PO number', err.message);
    } finally {
      savingPoNumber = false;
    }
  }

  function startEditPoNumber(current: string | null) {
    poNumberDraft = current ?? '';
    editingPoNumber = true;
  }

  function handlePoNumberKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') savePoNumber();
    if (e.key === 'Escape') { editingPoNumber = false; }
  }

  async function saveNotes() {
    if (savingNotes) return;
    savingNotes = true;
    try {
      const value = notesDraft.trim() || null;
      await updateOrderNotes(teamId, orderId, value);
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      editingNotes = false;
    } catch (err: any) {
      pushToast('error', 'Failed to save notes', err.message);
    } finally {
      savingNotes = false;
    }
  }

  function startEditNotes(current: string | null) {
    notesDraft = current ?? '';
    editingNotes = true;
  }

  function handleNotesKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') saveNotes();
    if (e.key === 'Escape') { editingNotes = false; }
  }

  function statusVariant(status: OrderStatus): 'secondary' | 'default' | 'warning' | 'success' {
    switch (status) {
      case 'draft': return 'secondary';
      case 'exported': return 'default';
      case 'submitted': return 'default';
      case 'placed': return 'default';
      case 'partially_received': return 'warning';
      case 'received': return 'success';
      case 'cancelled': return 'secondary';
      default: return 'secondary';
    }
  }

  function statusLabel(status: OrderStatus): string {
    switch (status) {
      case 'draft': return 'Draft';
      case 'exported': return 'Exported';
      case 'submitted': return 'Submitted';
      case 'placed': return 'Placed';
      case 'partially_received': return 'Partially Received';
      case 'received': return 'Received';
      case 'cancelled': return 'Cancelled';
      default: return status;
    }
  }

  function getReceiveQty(item: OrderLineItem): string {
    if (receiveQtys[item.id] !== undefined) return receiveQtys[item.id];
    const remaining = item.quantity_ordered - item.quantity_received;
    return String(remaining > 0 ? remaining : 0);
  }

  async function handleCancel() {
    cancelling = true;
    try {
      await cancelOrder(teamId, orderId);
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      qc.invalidateQueries({ queryKey: ['orders', teamId] });
      showCancelConfirm = false;
      pushToast('success', 'Order cancelled');
    } catch (err: any) {
      pushToast('error', 'Failed to cancel order', err.message);
    } finally {
      cancelling = false;
    }
  }

  async function handleMarkPlaced() {
    markingPlaced = true;
    try {
      await markOrderPlaced(teamId, orderId, placedRef.trim());
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      qc.invalidateQueries({ queryKey: ['orders', teamId] });
      showPlacedInput = false;
      placedRef = '';
      pushToast('success', 'Order marked as placed');
    } catch (err: any) {
      pushToast('error', 'Failed to mark order as placed', err.message);
    } finally {
      markingPlaced = false;
    }
  }

  async function executeReceive(item: OrderLineItem, qty: number) {
    receivingItems = new Set([...receivingItems, item.id]);
    try {
      const result = await receiveLineItem(teamId, orderId, item.id, qty);
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      qc.invalidateQueries({ queryKey: ['orders', teamId] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      const next = { ...receiveQtys };
      delete next[item.id];
      receiveQtys = next;
      if (result.order_status === 'received') {
        receivingMode = false;
      }
      pushToast('success', `Received ${qty} unit(s)`);
    } catch (err: any) {
      pushToast('error', 'Failed to receive items', err.message);
    } finally {
      const s = new Set(receivingItems);
      s.delete(item.id);
      receivingItems = s;
    }
  }

  async function handleReceive(item: OrderLineItem) {
    const raw = receiveQtys[item.id];
    const qty = raw !== undefined ? parseInt(raw, 10) : item.quantity_ordered - item.quantity_received;
    if (isNaN(qty) || qty <= 0) {
      pushToast('error', 'Quantity must be greater than 0');
      return;
    }
    if (item.quantity_received + qty > item.quantity_ordered) {
      overReceiveConfirm = { item, qty };
      return;
    }
    await executeReceive(item, qty);
  }

  let canReceive = $derived(
    orderQuery.data?.status === 'placed' ||
    orderQuery.data?.status === 'partially_received'
  );

  let scanOpen = $state(false);

  // Show a toast if returning from DigiKey OAuth flow.
  $effect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.has('oauth_success')) {
      pushToast('success', 'DigiKey account connected');
      const url = new URL(window.location.href);
      url.searchParams.delete('oauth_success');
      window.history.replaceState({}, '', url.toString());
    } else if (params.has('oauth_error')) {
      pushToast('error', 'DigiKey authorization failed', params.get('oauth_error') ?? '');
      const url = new URL(window.location.href);
      url.searchParams.delete('oauth_error');
      window.history.replaceState({}, '', url.toString());
    }
  });

  // Build a SKU → quoted unit price map from integrations for the inline column.
  const quotePriceBySku = $derived.by(() => {
    const items = orderQuery.data?.integrations?.digikey?.quote_items;
    if (!items?.length) return null;
    const map: Record<string, string> = {};
    for (const qi of items) {
      if (qi.unit_price) map[qi.sku] = qi.unit_price;
    }
    return map;
  });

  // Build a SKU → DigiKey status line item map for the inline status columns.
  const dkStatusBySku = $derived.by(() => {
    const items = orderQuery.data?.integrations?.digikey?.last_status_line_items;
    if (!items?.length) return null;
    const map = new Map<string, DigiKeyStatusLineItem>();
    for (const li of items) map.set(li.sku, li);
    return map;
  });

  const columns: ColumnSpec<OrderLineItem>[] = [
    { key: 'sku',                 header: 'SKU',              type: 'text',     filterable: true },
    { key: 'part_mpn',            header: 'Part MPN',         type: 'text',     filterable: true },
    { key: 'part_manufacturer',   header: 'Manufacturer',     type: 'text',     filterable: true },
    { key: 'part_description',    header: 'Description',      type: 'text' },
    { key: 'packaging_type',      header: 'Packaging',        type: 'enum',     filterable: true },
    { key: 'unit_price_snapshot', header: 'Unit Price',    type: 'currency', align: 'right' },
    { key: '_quoted_price',       header: 'Quoted Price',  type: 'currency', align: 'right', hidden: true },
    { key: '_total',              header: 'Total',         type: 'currency', align: 'right' },
    { key: 'quantity_ordered',    header: 'Ordered',       type: 'numeric',  align: 'right' },
    { key: 'quantity_received',   header: 'Received',      type: 'numeric',  align: 'right' },
    {
      key: '_receipt_status',
      header: 'Status',
      type: 'enum',
      sortable: false,
      filterable: true,
      filterValues: ['pending', 'partial', 'received'],
      filterLabels: { pending: 'Pending', partial: 'Partial', received: 'Received' },
    },
    { key: '_receive_qty',      header: 'Receive Qty',  type: 'enum',    sortable: false, hidden: true },
    { key: '_receive_action',   header: '',             type: 'enum',    sortable: false, hidden: true, width: '90px' },
  ];

  const source = new LocalDataSource<OrderLineItem>(
    () => orderQuery.data?.items ?? [],
    columns,
    (row, key) => {
      if (key === '_total') return parseFloat(row.unit_price_snapshot) * row.quantity_ordered;
      if (key === '_quoted_price') return quotePriceBySku?.[row.sku] ?? null;
      if (key === '_receipt_status') {
        if (row.quantity_received >= row.quantity_ordered) return 'received';
        if (row.quantity_received > 0) return 'partial';
        return 'pending';
      }
      return getNestedValue(row, key);
    },
  );

  $effect(() => {
    source.setColumnHidden('_receive_qty', !receivingMode);
    source.setColumnHidden('_receive_action', !receivingMode);
    source.setColumnHidden('_quoted_price', !quotePriceBySku);
  });

  function handleExportCSV() {
    const a = document.createElement('a');
    a.href = `/api/v1/teams/${teamId}/orders/${orderId}/export`;
    a.click();
    setTimeout(() => {
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      qc.invalidateQueries({ queryKey: ['orders', teamId] });
    }, 800);
  }
</script>

<div class="flex flex-col h-full">
  {#if orderQuery.data}
    {@const order = orderQuery.data}
    <Topbar
      title={order.id.slice(0, 8)}
      breadcrumbs={[
        { label: 'Orders', href: `/teams/${teamId}/orders` },
      ]}
    >
      {#snippet actions()}
        {#if order.status !== 'received' && order.status !== 'cancelled'}
          <Button variant="outline" class="text-destructive border-destructive hover:bg-destructive/10" size="sm" onclick={() => { showCancelConfirm = true; }}>
            Cancel Order
          </Button>
        {/if}
        {#if order.status !== 'received'}
          <Button variant="outline" size="sm" onclick={handleExportCSV}>
            {order.status === 'exported' ? 'Re-export CSV' : 'Export CSV'}
          </Button>
        {/if}
        {#if order.status === 'draft' || order.status === 'exported' || order.status === 'submitted'}
          {#if showPlacedInput}
            <Input
              value={placedRef}
              oninput={(e: Event) => { placedRef = (e.target as HTMLInputElement).value; }}
              placeholder="Order reference (optional)"
              class="w-56"
            />
            <Button size="sm" onclick={handleMarkPlaced} disabled={markingPlaced}>
              {markingPlaced ? 'Saving…' : 'Confirm Placed'}
            </Button>
            <Button variant="outline" size="sm" onclick={() => { showPlacedInput = false; placedRef = ''; }}>
              Cancel
            </Button>
          {:else}
            <Button size="sm" onclick={() => { showPlacedInput = true; }}>
              Mark as Placed
            </Button>
          {/if}
        {/if}
        {#if canReceive}
          <Button variant="outline" size="sm" onclick={() => { scanOpen = true; }}>
            <ScanLine class="mr-1.5 h-4 w-4" />
            Scan
          </Button>
          <Button
            size="sm"
            onclick={() => { receivingMode = !receivingMode; }}
          >
            {receivingMode ? 'Done Receiving' : 'Receive Items'}
          </Button>
        {/if}
      {/snippet}
    </Topbar>

    <ScanModal
      {teamId}
      orderId={order.id}
      bind:open={scanOpen}
    />

    <div class="flex-1 overflow-auto p-6 space-y-4">
      <MetadataCard title={order.id.slice(0, 8)}>
        {#snippet badges()}
          <Badge variant={statusVariant(order.status)}>{statusLabel(order.status)}</Badge>
        {/snippet}
        <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm sm:grid-cols-3">
          <div>
            <span class="text-muted-foreground">Distributor:</span>
            <span class="font-medium"> {order.distributor_name}</span>
          </div>
          <div class="flex items-center gap-1">
            <span class="text-muted-foreground">PO #:</span>
            {#if editingPoNumber}
              <Input
                class="h-6 w-36 text-xs px-1"
                value={poNumberDraft}
                oninput={(e: Event) => { poNumberDraft = (e.target as HTMLInputElement).value; }}
                onkeydown={handlePoNumberKeydown}
                onblur={savePoNumber}
                disabled={savingPoNumber}
                autofocus
              />
            {:else}
              <span class="font-medium font-mono text-xs">{order.purchase_order_number ?? '—'}</span>
              <Button variant="ghost" size="icon" class="h-5 w-5 ml-0.5" onclick={() => startEditPoNumber(order.purchase_order_number)}>
                <Pencil class="h-3 w-3" />
              </Button>
            {/if}
          </div>
          {#if order.external_reference}
            <div>
              <span class="text-muted-foreground">Distributor Order #:</span>
              <span class="font-medium font-mono text-xs"> {order.external_reference}</span>
            </div>
          {/if}
          <div>
            <span class="text-muted-foreground">Created:</span>
            <span class="font-medium"> <DateDisplay value={order.created_at} /></span>
          </div>
          <div>
            <span class="text-muted-foreground">Created by:</span>
            <span class="font-medium"> {order.created_by_display_name}</span>
          </div>
          <div>
            <span class="text-muted-foreground">Items:</span>
            <span class="font-medium tabular-nums"> {order.items?.length ?? 0}</span>
          </div>
          {#if order.total_price != null}
            <div>
              <span class="text-muted-foreground">Total:</span>
              <span class="font-medium"> <PriceDisplay value={order.total_price} decimals={2} /></span>
            </div>
          {/if}
          {#if order.received_at}
            <div>
              <span class="text-muted-foreground">Received:</span>
              <span class="font-medium"> <DateDisplay value={order.received_at} /></span>
            </div>
          {/if}
          {#if order.external_ref_linked_at}
            <div>
              <span class="text-muted-foreground">Placed:</span>
              <span class="font-medium"> <DateDisplay value={order.external_ref_linked_at} /></span>
            </div>
          {/if}
          {#if order.submission_method}
            <div>
              <span class="text-muted-foreground">Submission:</span>
              <span class="font-medium"> {order.submission_method}</span>
            </div>
          {/if}
          <div class="col-span-2 sm:col-span-3 flex items-center gap-1">
            <span class="text-muted-foreground">Notes:</span>
            {#if editingNotes}
              <Input
                class="h-6 flex-1 text-xs px-1"
                value={notesDraft}
                oninput={(e: Event) => { notesDraft = (e.target as HTMLInputElement).value; }}
                onkeydown={handleNotesKeydown}
                onblur={saveNotes}
                disabled={savingNotes}
                autofocus
              />
            {:else}
              <span class="font-medium text-xs">{order.notes ?? '—'}</span>
              <Button variant="ghost" size="icon" class="h-5 w-5 ml-0.5" onclick={() => startEditNotes(order.notes)}>
                <Pencil class="h-3 w-3" />
              </Button>
            {/if}
          </div>
        </div>
      </MetadataCard>

      {#if order.distributor_api_type === 'digikey'}
        <DigiKeyCard
          {teamId}
          {orderId}
          {order}
          returnTo={window.location.pathname}
        />
      {/if}

      {#if linkedBuildsQuery.data?.length}
        <div>
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide mb-3">Linked Builds</h3>
          <div class="rounded-md border divide-y">
            {#each linkedBuildsQuery.data as build}
              <button
                class="w-full text-left px-4 py-3 flex items-center gap-4 hover:bg-muted/50 transition-colors"
                onclick={() => navigate(`/teams/${teamId}/builds/${build.id}`)}
              >
                <div class="flex-1 min-w-0">
                  <div class="font-medium text-sm truncate">{build.project_name}</div>
                  <div class="text-xs text-muted-foreground">{build.bom_revision_label} · {build.quantity} board{build.quantity !== 1 ? 's' : ''}</div>
                </div>
                <div class="text-xs text-muted-foreground tabular-nums shrink-0">
                  {build.fulfilled_line_items}/{build.total_line_items} parts
                </div>
                <Badge variant={buildStatusVariant(build.status)}>{buildStatusLabel(build.status)}</Badge>
              </button>
            {/each}
          </div>
        </div>
      {/if}

      {#snippet renderSku(value: unknown)}
        <span class="font-mono text-xs">{String(value ?? '')}</span>
      {/snippet}
      {#snippet renderMpn(_value: unknown, row: OrderLineItem)}
        <a
          href={`/teams/${teamId}/parts/${row.part_id}`}
          class="font-mono font-medium text-xs hover:underline"
          onclick={(e) => e.stopPropagation()}
        >{row.part_mpn ?? '—'}</a>
      {/snippet}
      {#snippet renderUnitPrice(_value: unknown, row: OrderLineItem)}
        {@const dk = dkStatusBySku?.get(row.sku)}
        {@const differs = dk != null && !priceMatches(row.unit_price_snapshot, dk.unit_price)}
        <span class={differs ? 'text-amber-600 dark:text-amber-400' : ''} title={differs ? `DK: $${dk!.unit_price.toFixed(4)}` : undefined}>
          <PriceDisplay value={row.unit_price_snapshot} decimals={4} />
        </span>
      {/snippet}
      {#snippet renderPackaging(_value: unknown, row: OrderLineItem)}
        {@const dk = dkStatusBySku?.get(row.sku)}
        {@const differs = dk != null && dk.pack_type != null && !packagingMatches(row.packaging_type, dk.pack_type)}
        <span class={differs ? 'text-amber-600 dark:text-amber-400' : ''} title={differs ? `DK: ${dk!.pack_type}` : undefined}>
          {row.packaging_type ?? '—'}
        </span>
      {/snippet}
      {#snippet renderQuotedPrice(value: unknown)}
        {#if value != null}
          <PriceDisplay value={String(value)} decimals={4} />
        {:else}
          <span class="text-muted-foreground">—</span>
        {/if}
      {/snippet}
      {#snippet renderTotal(_value: unknown, row: OrderLineItem)}
        <PriceDisplay value={(parseFloat(row.unit_price_snapshot) * row.quantity_ordered).toFixed(2)} decimals={2} />
      {/snippet}
      {#snippet renderOrdered(_value: unknown, row: OrderLineItem)}
        {@const dk = dkStatusBySku?.get(row.sku)}
        {@const differs = dk != null && dk.quantity_ordered !== row.quantity_ordered}
        <span class={differs ? 'text-amber-600 dark:text-amber-400' : ''} title={differs ? `DK: ${dk!.quantity_ordered}` : undefined}>
          {row.quantity_ordered}
        </span>
      {/snippet}
      {#snippet renderReceiveQty(_value: unknown, row: OrderLineItem)}
        <Input
          type="number"
          value={getReceiveQty(row)}
          min="0"
          class="w-20"
          oninput={(e: Event) => {
            receiveQtys = { ...receiveQtys, [row.id]: (e.target as HTMLInputElement).value };
          }}
          disabled={receivingItems.has(row.id) || row.quantity_received >= row.quantity_ordered}
        />
      {/snippet}
      {#snippet renderReceiveAction(_value: unknown, row: OrderLineItem)}
        {@const fullyReceived = row.quantity_received >= row.quantity_ordered}
        <Button
          size="sm"
          onclick={() => handleReceive(row)}
          disabled={receivingItems.has(row.id) || fullyReceived}
        >
          {receivingItems.has(row.id) ? 'Saving…' : fullyReceived ? 'Received' : 'Receive'}
        </Button>
      {/snippet}
      {#snippet renderReceiptStatus(_value: unknown, row: OrderLineItem)}
        {#if row.quantity_received >= row.quantity_ordered}
          <Badge variant="success">Received</Badge>
        {:else if row.quantity_received > 0}
          <Badge variant="warning">Partial</Badge>
        {:else}
          <Badge variant="warning">Pending</Badge>
        {/if}
      {/snippet}

      {#snippet shipmentDetailPanel(row: OrderLineItem)}
        {@const dk = dkStatusBySku?.get(row.sku)}
        {#if dk}
          <div class="px-6 py-3 bg-muted/30 space-y-3">
            <div class="flex gap-6 text-sm">
              <div>
                <span class="text-muted-foreground">Shipped:</span>
                <span class="font-medium tabular-nums"> {dk.quantity_shipped}</span>
              </div>
              {#if dk.quantity_back_order > 0}
                <div>
                  <span class="text-muted-foreground">Backorder:</span>
                  <span class="font-medium tabular-nums text-amber-600 dark:text-amber-400"> {dk.quantity_back_order}</span>
                </div>
              {/if}
              {#if dk.quantity_reserved > 0}
                <div>
                  <span class="text-muted-foreground">Reserved:</span>
                  <span class="font-medium tabular-nums"> {dk.quantity_reserved}</span>
                </div>
              {/if}
            </div>

            {#if dk.shipments?.length}
              <div>
                <h4 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1.5">Shipments</h4>
                <div class="space-y-1.5">
                  {#each dk.shipments as shipment}
                    <div class="flex items-center gap-4 text-sm">
                      <div class="flex items-center gap-1.5">
                        <Package class="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                        <span class="tabular-nums">{shipment.quantity_shipped} pcs</span>
                      </div>
                      {#if shipment.shipped_date}
                        <div>
                          <span class="text-muted-foreground">Shipped:</span>
                          <span class="font-medium"> <DateDisplay value={shipment.shipped_date} /></span>
                        </div>
                      {/if}
                      {#if shipment.tracking_number}
                        {@const trackingUrl = getTrackingUrl(shipment.tracking_number, order.integrations?.digikey?.last_status_ship_method)}
                        <div class="flex items-center gap-1">
                          {#if trackingUrl}
                            <a
                              href={trackingUrl}
                              target="_blank"
                              rel="noopener noreferrer"
                              class="font-mono text-xs hover:underline"
                              onclick={(e) => e.stopPropagation()}
                            >{shipment.tracking_number}</a>
                          {:else}
                            <span class="font-mono text-xs">{shipment.tracking_number}</span>
                          {/if}
                          <button
                            type="button"
                            onclick={async (e) => {
                              e.stopPropagation();
                              try {
                                await navigator.clipboard.writeText(shipment.tracking_number);
                                pushToast('success', 'Copied to clipboard');
                              } catch {
                                pushToast('error', 'Failed to copy');
                              }
                            }}
                            class="text-muted-foreground hover:text-foreground transition-colors shrink-0"
                            title="Copy tracking number"
                          >
                            <Copy class="h-3 w-3" />
                          </button>
                        </div>
                      {/if}
                      {#if shipment.invoice_id}
                        <div class="text-muted-foreground text-xs">
                          Invoice #{shipment.invoice_id}
                        </div>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            {/if}

            {#if dk.schedules?.length}
              <div>
                <h4 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-1.5">Scheduled Releases</h4>
                <div class="space-y-1">
                  {#each dk.schedules as schedule}
                    <div class="flex items-center gap-4 text-sm">
                      <span class="tabular-nums">{schedule.quantity_scheduled} pcs</span>
                      {#if schedule.scheduled_date}
                        <div>
                          <span class="text-muted-foreground">Scheduled:</span>
                          <span class="font-medium"> <DateDisplay value={schedule.scheduled_date} /></span>
                        </div>
                      {/if}
                      {#if schedule.release_date}
                        <div>
                          <span class="text-muted-foreground">Release:</span>
                          <span class="font-medium"> <DateDisplay value={schedule.release_date} /></span>
                        </div>
                      {/if}
                    </div>
                  {/each}
                </div>
              </div>
            {/if}
          </div>
        {/if}
      {/snippet}

      <DataTable
        {source}
        getRowKey={(r) => r.id}
        emptyMessage="No line items"
        detailPanel={dkStatusBySku ? shipmentDetailPanel : undefined}
        detailPanelOpen={(row) => false}
        title="Line Items"
        renderers={{
          sku: renderSku,
          part_mpn: renderMpn,
          packaging_type: renderPackaging,
          unit_price_snapshot: renderUnitPrice,
          _quoted_price: renderQuotedPrice,
          _total: renderTotal,
          quantity_ordered: renderOrdered,
          _receipt_status: renderReceiptStatus,
          _receive_qty: renderReceiveQty,
          _receive_action: renderReceiveAction,
        }}
      />

    </div>

    <Dialog
      open={showCancelConfirm}
      title="Cancel this order?"
      confirmLabel={cancelling ? 'Cancelling…' : 'Cancel Order'}
      class="max-w-md"
      onConfirm={handleCancel}
      onCancel={() => { showCancelConfirm = false; }}
    >
      {#snippet children()}
        <p class="text-sm text-muted-foreground mb-4">
          This will mark the order as cancelled. Any items not yet received will remain unallocated.
          This action cannot be undone.
        </p>
      {/snippet}
    </Dialog>

    {#if overReceiveConfirm}
      {@const confirm = overReceiveConfirm}
      <Dialog
        open={true}
        title="Receive more than ordered?"
        confirmLabel="Confirm Receipt"
        class="max-w-md"
        onConfirm={() => {
          const { item, qty } = confirm;
          overReceiveConfirm = null;
          executeReceive(item, qty);
        }}
        onCancel={() => { overReceiveConfirm = null; }}
      >
        {#snippet children()}
          <p class="text-sm text-muted-foreground mb-4">
            You're about to receive <span class="font-medium text-foreground">{confirm.qty}</span> unit(s)
            of <span class="font-mono text-foreground">{confirm.item.sku}</span>,
            but only <span class="font-medium text-foreground">{Math.max(0, confirm.item.quantity_ordered - confirm.item.quantity_received)}</span> remain outstanding.
            The extra stock will still be added to inventory.
          </p>
        {/snippet}
      </Dialog>
    {/if}
  {:else if orderQuery.isPending}
    <div class="p-6 text-sm text-muted-foreground">Loading order…</div>
  {:else if orderQuery.isError}
    <div class="p-6 text-sm text-destructive">Error: {orderQuery.error.message}</div>
  {/if}
</div>

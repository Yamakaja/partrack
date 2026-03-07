<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getOrder, markOrderPlaced, receiveLineItem, cancelOrder, updateOrderPurchaseOrderNumber } from '$lib/api/orders.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import { DataTable, LocalDataSource, getNestedValue } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Pencil } from 'lucide-svelte';
  import type { OrderStatus, OrderLineItem } from '$lib/types/orders.js';

  let { teamId, orderId }: { teamId: string; orderId: string } = $props();

  const qc = useQueryClient();

  let orderQuery = createQuery(() => ({
    queryKey: ['order', orderId],
    queryFn: () => getOrder(teamId, orderId),
  }));

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

  const columns: ColumnSpec<OrderLineItem>[] = [
    { key: 'sku',                 header: 'SKU',         type: 'text',     filterable: true },
    { key: 'part_mpn',            header: 'Part MPN',    type: 'text',     filterable: true },
    { key: 'packaging_type',      header: 'Packaging',   type: 'enum',     filterable: true },
    { key: 'unit_price_snapshot', header: 'Unit Price',  type: 'currency', align: 'right' },
    { key: '_total',              header: 'Total',       type: 'currency', align: 'right' },
    { key: 'quantity_ordered',    header: 'Ordered',     type: 'numeric',  align: 'right' },
    { key: 'quantity_received',   header: 'Received',    type: 'numeric',  align: 'right' },
    {
      key: '_receipt_status',
      header: 'Status',
      type: 'enum',
      sortable: false,
      filterable: true,
      filterValues: ['pending', 'partial', 'received'],
      filterLabels: { pending: 'Pending', partial: 'Partial', received: 'Received' },
    },
    { key: '_receive_qty',        header: 'Receive Qty', type: 'enum',     sortable: false, hidden: true },
    { key: '_receive_action',     header: '',            type: 'enum',     sortable: false, hidden: true, width: '90px' },
  ];

  const source = new LocalDataSource<OrderLineItem>(
    () => orderQuery.data?.items ?? [],
    columns,
    (row, key) => {
      if (key === '_total') return parseFloat(row.unit_price_snapshot) * row.quantity_ordered;
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
      title={order.distributor_name}
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
          <Button
            size="sm"
            onclick={() => { receivingMode = !receivingMode; }}
          >
            {receivingMode ? 'Done Receiving' : 'Receive Items'}
          </Button>
        {/if}
      {/snippet}
    </Topbar>

    <div class="flex-1 overflow-auto p-6 space-y-4">
      <MetadataCard title={order.distributor_name}>
        {#snippet badges()}
          <Badge variant={statusVariant(order.status)}>{statusLabel(order.status)}</Badge>
        {/snippet}
        <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm sm:grid-cols-3">
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
            <span class="font-medium tabular-nums"> {order.line_item_count}</span>
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
          {#if order.notes}
            <div class="col-span-2 sm:col-span-3">
              <span class="text-muted-foreground">Notes:</span>
              <span class="font-medium"> {order.notes}</span>
            </div>
          {/if}
        </div>
      </MetadataCard>

      {#snippet renderSku(value: unknown)}
        <span class="font-mono text-xs">{String(value ?? '')}</span>
      {/snippet}
      {#snippet renderMpn(value: unknown)}
        <span class="font-mono font-medium text-xs">{value != null ? String(value) : '—'}</span>
      {/snippet}
      {#snippet renderUnitPrice(value: unknown)}
        <PriceDisplay value={String(value ?? '')} decimals={4} />
      {/snippet}
      {#snippet renderTotal(_value: unknown, row: OrderLineItem)}
        <PriceDisplay value={(parseFloat(row.unit_price_snapshot) * row.quantity_ordered).toFixed(2)} decimals={2} />
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

      <DataTable
        {source}
        getRowKey={(r) => r.id}
        emptyMessage="No line items"
        renderers={{
          sku: renderSku,
          part_mpn: renderMpn,
          unit_price_snapshot: renderUnitPrice,
          _total: renderTotal,
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

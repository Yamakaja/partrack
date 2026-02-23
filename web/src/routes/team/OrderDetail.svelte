<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getOrder, markOrderPlaced, receiveLineItem } from '$lib/api/orders.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
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
  let markingPlaced = $state(false);
  let placedRef = $state('');
  let showPlacedInput = $state(false);

  function statusVariant(status: OrderStatus): 'secondary' | 'default' | 'warning' | 'success' {
    switch (status) {
      case 'draft': return 'secondary';
      case 'placed': return 'default';
      case 'exported': return 'default';
      case 'partially_received': return 'warning';
      case 'received': return 'success';
      default: return 'secondary';
    }
  }

  function statusLabel(status: OrderStatus): string {
    switch (status) {
      case 'draft': return 'Draft';
      case 'placed': return 'Placed';
      case 'exported': return 'Exported';
      case 'partially_received': return 'Partially Received';
      case 'received': return 'Received';
      default: return status;
    }
  }

  function getReceiveQty(item: OrderLineItem): string {
    return receiveQtys[item.id] !== undefined
      ? receiveQtys[item.id]
      : String(item.quantity_ordered - item.quantity_received);
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

  async function handleReceive(item: OrderLineItem) {
    const raw = receiveQtys[item.id];
    const qty = raw !== undefined ? parseInt(raw, 10) : item.quantity_ordered - item.quantity_received;
    if (isNaN(qty) || qty < 0) {
      pushToast('error', 'Invalid quantity');
      return;
    }
    receivingItems = new Set([...receivingItems, item.id]);
    try {
      await receiveLineItem(teamId, orderId, item.id, qty);
      qc.invalidateQueries({ queryKey: ['order', orderId] });
      qc.invalidateQueries({ queryKey: ['orders', teamId] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      const next = { ...receiveQtys };
      delete next[item.id];
      receiveQtys = next;
      pushToast('success', `Received ${qty} unit(s)`);
    } catch (err: any) {
      pushToast('error', 'Failed to receive items', err.message);
    } finally {
      const s = new Set(receivingItems);
      s.delete(item.id);
      receivingItems = s;
    }
  }

  let canReceive = $derived(
    orderQuery.data?.status === 'placed' ||
    orderQuery.data?.status === 'partially_received'
  );

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
        { label: order.distributor_name },
      ]}
    >
      {#snippet actions()}
        {#if order.status === 'draft'}
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
            variant={receivingMode ? 'default' : 'outline'}
            size="sm"
            onclick={() => { receivingMode = !receivingMode; }}
          >
            {receivingMode ? 'Done Receiving' : 'Receive Items'}
          </Button>
        {/if}
        {#if order.status !== 'received'}
          <Button variant="outline" size="sm" onclick={handleExportCSV}>
            {order.status === 'exported' ? 'Re-export CSV' : 'Export CSV'}
          </Button>
        {/if}
      {/snippet}
    </Topbar>

    <div class="flex-1 overflow-auto p-6 space-y-4">
      <div class="flex items-center gap-4 text-sm">
        <span class="text-muted-foreground">Distributor:</span>
        <span class="font-medium">{order.distributor_name}</span>
        <span class="text-muted-foreground">Status:</span>
        <Badge variant={statusVariant(order.status)}>{statusLabel(order.status)}</Badge>
        {#if order.distributor_order_ref}
          <span class="text-muted-foreground">Ref:</span>
          <span class="font-mono text-xs">{order.distributor_order_ref}</span>
        {/if}
        <span class="text-muted-foreground">Created:</span>
        <DateDisplay value={order.created_at} />
      </div>

      {#if order.items && order.items.length > 0}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>SKU</TableHead>
              <TableHead>Part MPN</TableHead>
              <TableHead>Ordered</TableHead>
              <TableHead>Received</TableHead>
              {#if receivingMode}
                <TableHead>Receive Qty</TableHead>
                <TableHead></TableHead>
              {/if}
              <TableHead>Unit Price</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each order.items as item (item.id)}
              <TableRow>
                <TableCell class="font-mono text-xs">{item.sku}</TableCell>
                <TableCell class="font-mono font-medium text-xs">{item.part_mpn ?? '—'}</TableCell>
                <TableCell>{item.quantity_ordered}</TableCell>
                <TableCell>{item.quantity_received}</TableCell>
                {#if receivingMode}
                  <TableCell>
                    <Input
                      type="number"
                      value={getReceiveQty(item)}
                      min="0"
                      max={item.quantity_ordered}
                      class="w-20"
                      oninput={(e: Event) => {
                        receiveQtys = { ...receiveQtys, [item.id]: (e.target as HTMLInputElement).value };
                      }}
                      disabled={receivingItems.has(item.id)}
                    />
                  </TableCell>
                  <TableCell>
                    <Button
                      size="sm"
                      onclick={() => handleReceive(item)}
                      disabled={receivingItems.has(item.id) || item.quantity_received >= item.quantity_ordered}
                    >
                      {receivingItems.has(item.id) ? 'Saving…' : 'Receive'}
                    </Button>
                  </TableCell>
                {/if}
                <TableCell>
                  <PriceDisplay value={item.unit_price_snapshot} />
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {:else}
        <div class="text-sm text-muted-foreground">No line items.</div>
      {/if}
    </div>
  {:else if orderQuery.isPending}
    <div class="p-6 text-sm text-muted-foreground">Loading order…</div>
  {:else if orderQuery.isError}
    <div class="p-6 text-sm text-destructive">Error: {orderQuery.error.message}</div>
  {/if}
</div>

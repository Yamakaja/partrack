<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { listOrders } from '$lib/api/orders.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import type { OrderStatus } from '$lib/types/orders.js';

  let { teamId }: { teamId: string } = $props();

  let ordersQuery = createQuery(() => ({
    queryKey: ['orders', teamId],
    queryFn: () => listOrders(teamId),
  }));

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

  function shortId(id: string): string {
    return id.slice(0, 8);
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Orders" />

  <div class="flex-1 overflow-auto p-6">
    {#if ordersQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading orders…</div>
    {:else if ordersQuery.isError}
      <div class="text-sm text-destructive">Error: {ordersQuery.error.message}</div>
    {:else if ordersQuery.data}
      {@const items = ordersQuery.data.items}
      {#if items.length === 0}
        <EmptyState
          title="No orders yet"
          description="Convert your shopping cart to create orders."
        />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Distributor</TableHead>
              <TableHead>Status</TableHead>
              <TableHead># Items</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each items as order (order.id)}
              <TableRow
                class="cursor-pointer"
                onclick={() => navigate(`/teams/${teamId}/orders/${order.id}`)}
              >
                <TableCell class="font-mono text-xs text-muted-foreground">{shortId(order.id)}</TableCell>
                <TableCell class="font-medium">{order.distributor_name}</TableCell>
                <TableCell>
                  <Badge variant={statusVariant(order.status)}>{statusLabel(order.status)}</Badge>
                </TableCell>
                <TableCell class="text-muted-foreground">{order.line_item_count}</TableCell>
                <TableCell>
                  <DateDisplay value={order.created_at} />
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>

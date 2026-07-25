<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listOrders } from '$lib/api/orders.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import { statusBadge } from '$lib/objectref/registry.js';
  import ImportOrderDialog from '$lib/components/orders/ImportOrderDialog.svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();
  let showImportDialog = $state(false);

  let ordersQuery = createQuery(() => ({
    queryKey: ['orders', teamId],
    queryFn: () => listOrders(teamId),
  }));

  type OrderRow = NonNullable<typeof ordersQuery.data>['items'][number];


  const columns: ColumnSpec<OrderRow>[] = [
    {
      key: 'id',
      header: 'ID',
      type: 'text',
      sortable: false,
      width: '120px',
    },
    {
      key: 'distributor_name',
      header: 'Distributor',
      type: 'text',
      filterable: true,
    },
    {
      key: 'status',
      header: 'Status',
      type: 'enum',
      filterable: true,
      filterValues: ['draft', 'exported', 'submitted', 'placed', 'partially_received', 'received', 'cancelled'],
      filterLabels: {
        draft: 'Draft',
        exported: 'Exported',
        submitted: 'Submitted',
        placed: 'Placed',
        partially_received: 'Partially Received',
        received: 'Received',
        cancelled: 'Cancelled',
      },
      exportValue: (row) => statusBadge('order', row.status)?.label ?? row.status,
    },
    {
      key: 'line_item_count',
      header: '# Items',
      type: 'numeric',
      align: 'right',
      filterable: true,
    },
    {
      key: 'total_price',
      header: 'Total',
      type: 'numeric',
      align: 'right',
      filterable: true,
    },
    {
      key: 'created_by_display_name',
      header: 'Created By',
      type: 'text',
    },
    {
      key: 'created_at',
      header: 'Created',
      type: 'date',
      filterable: true,
      exportValue: (row) => new Date(row.created_at).toISOString(),
    },
  ];

  const source = new LocalDataSource<OrderRow>(
    () => ordersQuery.data?.items ?? [],
    columns,
  );
</script>

{#snippet renderId(value: unknown, _row: OrderRow)}
  <span class="font-mono text-xs text-muted-foreground">{String(value).slice(0, 8)}</span>
{/snippet}

{#snippet renderStatus(value: unknown, _row: OrderRow)}
  {@const sb = statusBadge('order', value as string)}
  <Badge variant={sb?.variant as never}>
    {sb?.label}
  </Badge>
{/snippet}

{#snippet renderTotalPrice(value: unknown, _row: OrderRow)}
  <PriceDisplay value={value as string | null} />
{/snippet}

{#snippet renderCreatedAt(value: unknown, _row: OrderRow)}
  <DateDisplay value={String(value)} />
{/snippet}

<ImportOrderDialog
  {teamId}
  bind:open={showImportDialog}
  onSuccess={() => qc.invalidateQueries({ queryKey: ['orders', teamId] })}
/>

<div class="flex flex-col h-full">
  <Topbar title="Orders">
    {#snippet actions()}
      <Button size="sm" onclick={() => { showImportDialog = true; }}>
        Import DigiKey Order
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if ordersQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading orders…</div>
    {:else if ordersQuery.isError}
      <div class="text-sm text-destructive">Error: {ordersQuery.error.message}</div>
    {:else}
      <DataTable
        {source}
        getRowKey={(o) => o.id}
        onRowClick={(o) => navigate(`/teams/${teamId}/orders/${o.id}`)}
        renderers={{ id: renderId, status: renderStatus, total_price: renderTotalPrice, created_at: renderCreatedAt }}
        emptyMessage="No orders yet"
        exportFilename="orders.csv"
      />
    {/if}
  </div>
</div>

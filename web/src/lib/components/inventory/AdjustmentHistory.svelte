<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { listAdjustments } from '$lib/api/inventory.js';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import Pagination from '$lib/components/common/Pagination.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
    import { linkAction } from '$lib/router/index.svelte';

  let {
    entryId,
    teamId,
  }: {
    entryId: string;
    teamId: string;
  } = $props();

  const limit = 20;
  let offset = $state(0);

  let query = createQuery(() => ({
    queryKey: ['adjustments', teamId, entryId, limit, offset],
    queryFn: () => listAdjustments(teamId, entryId, { limit, offset }),
  }));
</script>

<div class="space-y-3">
  {#if query.isPending}
    <div class="text-sm text-muted-foreground">Loading…</div>
  {:else if query.isError}
    <div class="text-sm text-destructive">Error: {query.error.message}</div>
  {:else if query.data}
    {@const data = query.data}
    {#if data.items.length === 0}
      <EmptyState title="No adjustments yet" description="Stock adjustments will appear here." />
    {:else}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Date</TableHead>
            <TableHead>Delta</TableHead>
            <TableHead>Reason</TableHead>
            <TableHead>Source</TableHead>
            <TableHead>By</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each data.items as adj (adj.id)}
            <TableRow>
              <TableCell>
                <DateDisplay value={adj.adjusted_at} />
              </TableCell>
              <TableCell>
                <span class={adj.delta >= 0 ? 'text-green-600 dark:text-green-400 font-medium' : 'text-red-600 dark:text-red-400 font-medium'}>
                  {adj.delta >= 0 ? '+' : ''}{adj.delta}
                </span>
              </TableCell>
              <TableCell class="text-muted-foreground">{adj.reason ?? '—'}</TableCell>
              <TableCell class="text-muted-foreground">
                {#if adj.source_type === 'order_line_item' && adj.source_order_id}
                  <a use:linkAction href={`/teams/${teamId}/orders/${adj.source_order_id}`} class="underline hover:text-foreground">Order</a>
                {:else if adj.source_type === 'build'}
                  <a use:linkAction href={`/teams/${teamId}/builds/${adj.source_id}`} class="underline hover:text-foreground">Build</a>
                {:else if adj.source_type === 'general_checkout'}
                  Checkout
                {:else}
                  Manual
                {/if}
              </TableCell>
              <TableCell class="text-muted-foreground">{adj.created_by_display_name}</TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
      <Pagination
        total={data.total}
        {limit}
        {offset}
        onchange={(o) => (offset = o)}
      />
    {/if}
  {/if}
</div>

<script lang="ts">
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import { effectiveOrderQty } from '$lib/utils/cart.js';
  import type { CartItem } from '$lib/types/orders.js';

  let { distributorGroups, distributorTotals, summaryGrandTotals }: {
    distributorGroups: Map<string, CartItem[]>;
    distributorTotals: Map<string, { subtotal: string; fixed_order_cost: string; total: string }>;
    summaryGrandTotals: { positions: number; parts: number; subtotal: string; total: string };
  } = $props();
</script>

<Table>
  <TableHeader>
    <TableRow>
      <TableHead>Distributor</TableHead>
      <TableHead class="text-right">Positions</TableHead>
      <TableHead class="text-right">Total Parts</TableHead>
      <TableHead class="text-right">Parts Subtotal</TableHead>
      <TableHead class="text-right">Fixed Cost</TableHead>
      <TableHead class="text-right">Order Total</TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each [...distributorGroups.entries()] as [distributor, distItems]}
      {@const distTotal = distributorTotals.get(distributor)}
      {@const isUnassigned = distributor === 'Unassigned'}
      {@const totalParts = distItems.reduce((s, i) => s + effectiveOrderQty(i), 0)}
      <TableRow>
        <TableCell>
          <div class="flex items-center gap-2">
            <span class="font-medium">{distributor}</span>
            {#if isUnassigned}<Badge variant="warning" class="text-xs">No listing</Badge>{/if}
          </div>
        </TableCell>
        <TableCell class="text-right tabular-nums">{distItems.length}</TableCell>
        <TableCell class="text-right tabular-nums">
          {#if isUnassigned}<span class="text-muted-foreground">—</span>{:else}{totalParts.toLocaleString()}{/if}
        </TableCell>
        <TableCell class="text-right tabular-nums">
          {#if distTotal && !isUnassigned}
            <PriceDisplay value={distTotal.subtotal} />
          {:else}
            <span class="text-muted-foreground">—</span>
          {/if}
        </TableCell>
        <TableCell class="text-right tabular-nums text-muted-foreground">
          {#if distTotal && !isUnassigned && distTotal.fixed_order_cost !== '0.00'}
            <PriceDisplay value={distTotal.fixed_order_cost} />
          {:else}
            <span>—</span>
          {/if}
        </TableCell>
        <TableCell class="text-right tabular-nums font-semibold">
          {#if distTotal && !isUnassigned}
            <PriceDisplay value={distTotal.total} />
          {:else}
            <span class="text-muted-foreground">—</span>
          {/if}
        </TableCell>
      </TableRow>
    {/each}
  </TableBody>
  {#if distributorGroups.size > 1}
    <TableBody>
      <TableRow class="border-t-2 font-semibold">
        <TableCell class="text-muted-foreground">Grand Total</TableCell>
        <TableCell class="text-right tabular-nums">{summaryGrandTotals.positions}</TableCell>
        <TableCell class="text-right tabular-nums">{summaryGrandTotals.parts.toLocaleString()}</TableCell>
        <TableCell class="text-right tabular-nums"><PriceDisplay value={summaryGrandTotals.subtotal} /></TableCell>
        <TableCell></TableCell>
        <TableCell class="text-right tabular-nums"><PriceDisplay value={summaryGrandTotals.total} /></TableCell>
      </TableRow>
    </TableBody>
  {/if}
</Table>

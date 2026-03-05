<script lang="ts">
  import { TableRow, TableCell } from '$lib/components/ui/table/index.js';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import PriceBreaksTable from '$lib/components/common/PriceBreaksTable.svelte';
  import {
    effectiveUnitPriceStr, itemDistributor,
    itemSku, itemLastRefreshed,
  } from '$lib/utils/cart.js';
  import type { CartItem } from '$lib/types/orders.js';

  let { item, orderQty, moq, demand }: {
    item: CartItem;
    orderQty: number;
    moq: number;
    demand: number;
  } = $props();

  let breaks = $derived(item.listing?.price_breaks ?? []);
  let packaging = $derived(item.listing?.packaging_type ?? null);
  let stock = $derived(item.listing?.stock_available ?? null);
  let refreshed = $derived(itemLastRefreshed(item));
  let distributor = $derived(itemDistributor(item));
  let sku = $derived(itemSku(item));
  let demands = $derived(item.build_demands ?? []);
</script>

<TableRow class="bg-muted/30 hover:bg-muted/30">
  <TableCell colspan={8} class="py-3 px-4">
    <div class="flex divide-x divide-border">

      <!-- Listing Details -->
      <div class="space-y-2 shrink-0 pr-8">
        <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Listing Details</p>
        <dl class="text-xs grid grid-cols-[5.5rem_auto] gap-x-4 gap-y-1">
          <dt class="text-muted-foreground">Distributor</dt>
          <dd class="font-medium">{distributor}</dd>
          {#if sku}
            <dt class="text-muted-foreground">SKU</dt>
            <dd class="font-mono">{sku}</dd>
          {/if}
          {#if packaging}
            <dt class="text-muted-foreground">Packaging</dt>
            <dd>{packaging}</dd>
          {/if}
          <dt class="text-muted-foreground">MOQ</dt>
          <dd class={moq > 0 && item.quantity < moq ? 'font-medium text-destructive' : 'font-medium'}>
            {moq > 0 ? moq : '—'}
          </dd>
          <dt class="text-muted-foreground">Stock</dt>
          <dd class={stock !== null && stock === 0 ? 'text-destructive font-medium' : 'font-medium'}>
            {#if stock === null}
              <span class="text-muted-foreground">Unknown</span>
            {:else}
              {stock.toLocaleString()}
            {/if}
          </dd>
          <dt class="text-muted-foreground">Refreshed</dt>
          <dd class="text-muted-foreground">{refreshed ?? 'Never'}</dd>
          <dt class="text-muted-foreground">Fixed cost</dt>
          <dd>
            <PriceDisplay value={item.listing?.fixed_order_cost} />
          </dd>
        </dl>
      </div>

      <!-- Price Breaks -->
      <div class="space-y-2 shrink-0 px-8">
        <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Price Breaks</p>
        <PriceBreaksTable {breaks} qty={orderQty} prevQty={item.quantity} flatPrice={effectiveUnitPriceStr(item, orderQty)} />
      </div>

      <!-- Build Demand -->
      <div class="space-y-2 shrink-0 pl-8">
        <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Build Demand</p>
        {#if item.source === 'bom_shortfall' && demands.length > 0}
          <table class="text-xs">
            <thead>
              <tr class="text-muted-foreground">
                <th class="text-left font-normal pb-1 pr-4">Project</th>
                <th class="text-left font-normal pb-1 pr-4">Rev</th>
                <th class="text-right font-normal pb-1">Needed</th>
              </tr>
            </thead>
            <tbody>
              {#each demands as d}
                <tr>
                  <td class="py-0.5 pr-4 truncate max-w-[10rem]" title={d.project_name}>{d.project_name}</td>
                  <td class="py-0.5 pr-4 font-mono text-muted-foreground">{d.bom_revision_label}</td>
                  <td class="py-0.5 text-right tabular-nums">{d.quantity_needed.toLocaleString()}</td>
                </tr>
              {/each}
            </tbody>
            <tfoot>
              <tr class="border-t border-border text-muted-foreground">
                <td colspan={2} class="pt-1 pr-4">Total demand</td>
                <td class="pt-1 text-right tabular-nums font-medium text-foreground">{demand.toLocaleString()}</td>
              </tr>
              <tr class="text-muted-foreground">
                <td colspan={2} class="pr-4">Ordering</td>
                <td class="text-right tabular-nums font-medium text-foreground">
                  {orderQty.toLocaleString()}
                  {#if orderQty > item.quantity}
                    <span class="ml-1 text-emerald-600 dark:text-emerald-400">(price break)</span>
                  {/if}
                </td>
              </tr>
              {#if orderQty > demand && demand > 0}
                <tr class="text-muted-foreground">
                  <td colspan={2} class="pr-4">Excess</td>
                  <td class="text-right tabular-nums">
                    +{(orderQty - demand).toLocaleString()}
                    {#if moq > 0 && orderQty <= moq}
                      <span class="ml-1">(MOQ)</span>
                    {/if}
                  </td>
                </tr>
              {/if}
            </tfoot>
          </table>
        {:else if item.source === 'inventory_maintenance'}
          <p class="text-xs text-muted-foreground">Replenishment order — not tied to a build.</p>
        {:else if item.source === 'manual'}
          <p class="text-xs text-muted-foreground">Manually added item.</p>
        {:else}
          <p class="text-xs text-muted-foreground">No pending build demand for this part.</p>
        {/if}
      </div>

    </div>
  </TableCell>
</TableRow>

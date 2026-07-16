<script lang="ts">
  import { TableRow, TableCell } from '$lib/components/ui/table/index.js';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import PartRef from '$lib/components/parts/PartRef.svelte';
  import PriceBreaksTable from '$lib/components/common/PriceBreaksTable.svelte';
  import {
    effectiveUnitPriceStr, itemDistributor,
    itemSku, itemLastRefreshed,
  } from '$lib/utils/cart.js';
  import type { CartItem } from '$lib/types/orders.js';

  let { item, teamId, orderQty, moq, demand, companionQty = 0 }: {
    item: CartItem;
    teamId?: string;
    orderQty: number;
    moq: number;
    demand: number;
    companionQty?: number;
  } = $props();

  let breaks = $derived(item.listing?.price_breaks ?? []);
  let packaging = $derived(item.listing?.packaging_type ?? null);
  let stock = $derived(item.listing?.stock_available ?? null);
  let refreshed = $derived(itemLastRefreshed(item));
  let distributor = $derived(itemDistributor(item));
  let sku = $derived(itemSku(item));
  let demands = $derived(item.build_demands ?? []);

  // Qty breakdown steps
  let available = $derived(demand - item.quantity); // stock covering some demand (> 0 means partial coverage)
  let combinedDemand = $derived(item.quantity + companionQty);
  let moqBump = $derived(moq > combinedDemand ? moq - combinedDemand : 0);
  let afterMoq = $derived(Math.max(combinedDemand, moq));
  let priceBreakBump = $derived(orderQty > afterMoq ? orderQty - afterMoq : 0);
</script>

<TableRow class="bg-muted/30 hover:bg-muted/30">
  <TableCell colspan={9} class="py-3 pr-4 pl-0">
    <div class="flex divide-x divide-border">

      <!-- Product Image -->
      {#if item.part_image_url}
        <div class="shrink-0 px-8 flex items-center justify-center">
          <img
            src={item.part_image_url}
            alt={item.part_mpn ?? 'Part image'}
            class="h-32 w-32 object-contain rounded border border-border bg-white p-2"
          />
        </div>
      {/if}

      <!-- Part Details -->
      <div class="space-y-2 shrink-0 px-8">
        <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Part Details</p>
        <dl class="text-xs grid grid-cols-[5.5rem_auto] gap-x-4 gap-y-1">
          {#if item.part_mpn}
            <dt class="text-muted-foreground">MPN</dt>
            <dd><PartRef partId={item.part_id} mpn={item.part_mpn} {teamId} /></dd>
          {/if}
          {#if item.part_manufacturer}
            <dt class="text-muted-foreground">Manufacturer</dt>
            <dd class="font-medium">{item.part_manufacturer}</dd>
          {/if}
          {#if item.part_package}
            <dt class="text-muted-foreground">Package</dt>
            <dd class="font-mono">{item.part_package}</dd>
          {/if}
          {#if item.part_category}
            <dt class="text-muted-foreground">Category</dt>
            <dd>{item.part_category}</dd>
          {/if}
          {#if item.part_short_description ?? item.part_description}
            <dt class="text-muted-foreground">Description</dt>
            <dd class="max-w-[16rem] leading-snug">{item.part_short_description ?? item.part_description}</dd>
          {/if}
          {#if item.part_product_url}
            <dt class="text-muted-foreground">Product page</dt>
            <dd><a href={item.part_product_url} target="_blank" rel="noopener noreferrer" class="text-primary underline underline-offset-2">View ↗</a></dd>
          {/if}
          {#if item.part_datasheet_url}
            <dt class="text-muted-foreground">Datasheet</dt>
            <dd><a href={item.part_datasheet_url} target="_blank" rel="noopener noreferrer" class="text-primary underline underline-offset-2">PDF ↗</a></dd>
          {/if}
        </dl>
      </div>

      <!-- Listing Details -->
      <div class="space-y-2 shrink-0 px-8">
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
        <PriceBreaksTable {breaks} qty={orderQty} prevQty={combinedDemand} flatPrice={effectiveUnitPriceStr(item, orderQty)} />
      </div>

      <!-- Order Breakdown -->
      <div class="space-y-2 shrink-0 pl-8">
        <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Order Breakdown</p>
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
                <td colspan={2} class="pt-1 pr-4">Build demand</td>
                <td class="pt-1 text-right tabular-nums font-medium text-foreground">{demand.toLocaleString()}</td>
              </tr>
              {#if available > 0}
                <tr class="text-muted-foreground">
                  <td colspan={2} class="pr-4">Available</td>
                  <td class="text-right tabular-nums text-success">−{available.toLocaleString()}</td>
                </tr>
              {/if}
              {#if companionQty > 0}
                <tr class="text-muted-foreground">
                  <td colspan={2} class="pr-4">Manual</td>
                  <td class="text-right tabular-nums font-medium text-foreground">+{companionQty.toLocaleString()}</td>
                </tr>
              {/if}
              {#if moqBump > 0}
                <tr class="text-muted-foreground">
                  <td colspan={2} class="pr-4">MOQ</td>
                  <td class="text-right tabular-nums">+{moqBump.toLocaleString()}</td>
                </tr>
              {/if}
              {#if priceBreakBump > 0}
                <tr class="text-muted-foreground">
                  <td colspan={2} class="pr-4">Price break</td>
                  <td class="text-right tabular-nums text-success">+{priceBreakBump.toLocaleString()}</td>
                </tr>
              {/if}
              <tr class="border-t border-border">
                <td colspan={3} class="pt-0"></td>
              </tr>
              <tr>
                <td colspan={2} class="pr-4 text-muted-foreground">Ordering</td>
                <td class="text-right tabular-nums font-semibold text-foreground">{orderQty.toLocaleString()}</td>
              </tr>
            </tfoot>
          </table>
        {:else}
          <!-- inventory_maintenance or manual: show compact breakdown -->
          <table class="text-xs">
            <tbody class="text-muted-foreground">
              {#if item.source === 'inventory_maintenance'}
                <tr>
                  <td class="pr-8 pb-0.5">Replenishment</td>
                  <td class="text-right tabular-nums font-medium text-foreground">{item.quantity.toLocaleString()}</td>
                </tr>
              {:else}
                <tr>
                  <td class="pr-8 pb-0.5">Requested</td>
                  <td class="text-right tabular-nums font-medium text-foreground">{item.quantity.toLocaleString()}</td>
                </tr>
              {/if}
              {#if companionQty > 0}
                <tr>
                  <td class="pr-8 pb-0.5">Manual</td>
                  <td class="text-right tabular-nums">+{companionQty.toLocaleString()}</td>
                </tr>
              {/if}
              {#if moqBump > 0}
                <tr>
                  <td class="pr-8 pb-0.5">MOQ</td>
                  <td class="text-right tabular-nums">+{moqBump.toLocaleString()}</td>
                </tr>
              {/if}
              {#if priceBreakBump > 0}
                <tr>
                  <td class="pr-8 pb-0.5">Price break</td>
                  <td class="text-right tabular-nums text-success">+{priceBreakBump.toLocaleString()}</td>
                </tr>
              {/if}
            </tbody>
            <tfoot>
              <tr class="border-t border-border">
                <td colspan={2} class="pt-0"></td>
              </tr>
              <tr>
                <td class="pr-8 text-muted-foreground">Ordering</td>
                <td class="text-right tabular-nums font-semibold text-foreground">{orderQty.toLocaleString()}</td>
              </tr>
            </tfoot>
          </table>
        {/if}
      </div>

    </div>
  </TableCell>
</TableRow>

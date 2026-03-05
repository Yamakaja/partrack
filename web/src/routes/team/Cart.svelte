<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    getCart, updateCartItem, removeCartItem,
    addLowStock, convertCart,
  } from '$lib/api/cart.js';
  import { listBuilds, toggleBuildCartInclusion } from '$lib/api/builds.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw, ArrowRight, Trash2, ExternalLink, ChevronDown, ChevronRight } from 'lucide-svelte';
  import type { CartItem, BuildDemand, PriceBreak } from '$lib/types/orders.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let cartQuery = createQuery(() => ({
    queryKey: ['cart', teamId],
    queryFn: () => getCart(teamId),
  }));

  let pendingBuildsQuery = createQuery(() => ({
    queryKey: ['builds', teamId, 'pending'],
    queryFn: () => listBuilds(teamId, { status: 'pending' }),
  }));

  let qtyEdits = $state<Record<string, string>>({});
  let updatingItems = $state<Set<string>>(new Set());
  let removingItems = $state<Set<string>>(new Set());
  let togglingBuilds = $state<Set<string>>(new Set());
  let addingLowStock = $state(false);
  let converting = $state(false);
  let expandedItemId = $state<string | null>(null);

  function toggleExpand(itemId: string) {
    expandedItemId = expandedItemId === itemId ? null : itemId;
  }

  function getQty(item: CartItem): string {
    return qtyEdits[item.id] !== undefined ? qtyEdits[item.id] : String(item.quantity);
  }

  function handleQtyInput(itemId: string, val: string) {
    qtyEdits = { ...qtyEdits, [itemId]: val };
  }

  async function commitQtyUpdate(item: CartItem) {
    const raw = qtyEdits[item.id];
    if (raw === undefined) return;
    const qty = parseInt(raw, 10);
    if (isNaN(qty) || qty < 1 || qty === item.quantity) {
      const next = { ...qtyEdits };
      delete next[item.id];
      qtyEdits = next;
      return;
    }
    updatingItems = new Set([...updatingItems, item.id]);
    try {
      await updateCartItem(teamId, item.id, { quantity: qty });
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      const next = { ...qtyEdits };
      delete next[item.id];
      qtyEdits = next;
    } catch (err: any) {
      pushToast('error', 'Failed to update quantity', err.message);
    } finally {
      const s = new Set(updatingItems);
      s.delete(item.id);
      updatingItems = s;
    }
  }

  async function handleRemove(itemId: string) {
    removingItems = new Set([...removingItems, itemId]);
    try {
      await removeCartItem(teamId, itemId);
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
    } catch (err: any) {
      pushToast('error', 'Failed to remove item', err.message);
    } finally {
      const s = new Set(removingItems);
      s.delete(itemId);
      removingItems = s;
    }
  }

  async function handleToggleInclusion(buildId: string, newExcluded: boolean) {
    togglingBuilds = new Set([...togglingBuilds, buildId]);
    try {
      await toggleBuildCartInclusion(teamId, buildId, newExcluded);
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
    } catch (err: any) {
      pushToast('error', 'Failed to update build inclusion', err.message);
    } finally {
      const s = new Set(togglingBuilds);
      s.delete(buildId);
      togglingBuilds = s;
    }
  }

  async function handleAddLowStock() {
    addingLowStock = true;
    try {
      const result = await addLowStock(teamId);
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      pushToast('success', `Added ${result.added_count} low-stock item(s) to cart`);
    } catch (err: any) {
      pushToast('error', 'Failed to add low-stock items', err.message);
    } finally {
      addingLowStock = false;
    }
  }

  async function handleConvert() {
    converting = true;
    try {
      const result = await convertCart(teamId);
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId] });
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      pushToast('success', `Created ${result.order_ids.length} order(s)`);
      navigate(`/teams/${teamId}/orders`);
    } catch (err: any) {
      pushToast('error', 'Failed to convert cart', err.message);
    } finally {
      converting = false;
    }
  }

  // --- Item helpers ---

  // Returns the quantity that ConvertCart will actually order for this item,
  // which may exceed item.quantity when a price break makes ordering more cheaper overall.
  // The backend computes optimized_qty for both explicit-listing and resolved-listing items.
  function effectiveOrderQty(item: CartItem): number {
    if (item.listing_id) {
      return item.optimized_qty ?? Math.max(item.quantity, item.listing_moq ?? 0);
    }
    return Math.max(item.quantity, item.resolved_listing?.optimized_qty ?? 0);
  }

  // orderQty is the quantity ConvertCart will actually place — may exceed item.quantity
  // when a price break makes a larger order cheaper. Callers should use this for both
  // price break lookups and subtotal calculations.
  function effectiveUnitPrice(item: CartItem, orderQty: number): number {
    // For resolved listings the backend already computed the price at optimalQty.
    if (!item.listing_id && item.resolved_listing?.unit_price) {
      return parseFloat(item.resolved_listing.unit_price);
    }
    let price = item.unit_price ? parseFloat(item.unit_price) : NaN;
    for (const pb of item.price_breaks ?? []) {
      if (orderQty >= pb.break_qty) price = parseFloat(pb.unit_price);
    }
    return isNaN(price) ? 0 : price;
  }

  function effectiveUnitPriceStr(item: CartItem, orderQty: number): string | null {
    const price = effectiveUnitPrice(item, orderQty);
    return price > 0 ? price.toFixed(4) : null;
  }

  function calcSubtotal(item: CartItem, orderQty: number): string | null {
    const hasPrice = item.unit_price || item.price_breaks?.length || item.resolved_listing?.unit_price;
    if (!hasPrice) return null;
    const price = effectiveUnitPrice(item, orderQty);
    if (price === 0) return null;
    return (price * orderQty).toFixed(2);
  }

  function itemDistributor(item: CartItem): string {
    return item.listing_distributor_name
      ?? item.resolved_listing?.distributor_name
      ?? 'Unassigned';
  }

  function itemSku(item: CartItem): string | null {
    return item.listing_sku ?? item.resolved_listing?.sku ?? null;
  }

  function itemMoq(item: CartItem): number {
    if (item.listing_id) return item.listing_moq ?? 0;
    return item.resolved_listing?.moq ?? 0;
  }

  function itemPriceBreaks(item: CartItem): PriceBreak[] {
    return item.listing_id ? (item.price_breaks ?? []) : (item.resolved_listing?.price_breaks ?? []);
  }

  function itemPackagingType(item: CartItem): string | null {
    if (item.listing_id) return item.listing_packaging_type ?? null;
    return item.resolved_listing?.packaging_type ?? null;
  }

  function itemStockAvailable(item: CartItem): number | null {
    if (item.listing_id) return item.listing_stock_available ?? null;
    return item.resolved_listing?.stock_available ?? null;
  }

  function itemLastRefreshed(item: CartItem): string | null {
    const ts = item.listing_last_refreshed_at;
    if (!ts) return null;
    const diff = Date.now() - new Date(ts).getTime();
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    return `${Math.floor(hrs / 24)}d ago`;
  }

  function totalBuildDemand(item: CartItem): number {
    return (item.build_demands ?? []).reduce((s, d) => s + d.quantity_needed, 0);
  }

  function activePriceBreakQty(item: CartItem, orderQty: number): number {
    const breaks = itemPriceBreaks(item);
    let active = -1;
    for (const pb of breaks) {
      if (orderQty >= pb.break_qty && pb.break_qty > active) active = pb.break_qty;
    }
    return active;
  }

  // --- Grouped / derived ---

  let distributorGroups = $derived.by(() => {
    const items = cartQuery.data?.items ?? [];
    const groups = new Map<string, CartItem[]>();
    for (const item of items) {
      const key = itemDistributor(item);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push(item);
    }
    return groups;
  });

  let distributorTotals = $derived.by(() => {
    const result = new Map<string, { subtotal: string; fixed_order_cost: string; total: string }>();
    for (const [dist, items] of distributorGroups) {
      let subtotal = 0;
      let fixedCost = 0;
      for (const item of items) {
        const oq = effectiveOrderQty(item);
        subtotal += effectiveUnitPrice(item, oq) * oq;
        const fcStr = item.listing_id
          ? (item.fixed_order_cost ?? '0')
          : (item.resolved_listing?.fixed_order_cost ?? '0');
        const fc = parseFloat(fcStr);
        if (!isNaN(fc) && fc > fixedCost) fixedCost = fc;
      }
      result.set(dist, {
        subtotal: subtotal.toFixed(2),
        fixed_order_cost: fixedCost.toFixed(2),
        total: (subtotal + fixedCost).toFixed(2),
      });
    }
    return result;
  });

  // Per-build estimated ordering cost: sum of (quantity_needed × unit_price) across all cart items.
  // Uses the optimized unit price since that's what will actually be paid.
  let buildOrderCosts = $derived.by(() => {
    const costs = new Map<string, number>();
    for (const item of cartQuery.data?.items ?? []) {
      const unitPrice = effectiveUnitPrice(item, effectiveOrderQty(item));
      if (!unitPrice) continue;
      for (const d of item.build_demands ?? []) {
        costs.set(d.build_id, (costs.get(d.build_id) ?? 0) + d.quantity_needed * unitPrice);
      }
    }
    return costs;
  });

  let manualItems = $derived(
    (cartQuery.data?.items ?? []).filter(i => i.source === 'manual')
  );

  let pendingBuilds = $derived(pendingBuildsQuery.data?.items ?? []);
  let isEmpty = $derived(
    pendingBuilds.length === 0 && (cartQuery.data?.items?.length ?? 0) === 0
  );
</script>

<div class="flex flex-col h-full">
  <Topbar title="Shopping Cart">
    {#snippet actions()}
      <Button
        variant="outline"
        size="sm"
        onclick={handleAddLowStock}
        disabled={addingLowStock}
      >
        <RefreshCw class="h-4 w-4 mr-1" />
        {addingLowStock ? 'Adding…' : 'Add Low Stock'}
      </Button>
      {#if (cartQuery.data?.items?.length ?? 0) > 0}
        <Button
          size="sm"
          onclick={handleConvert}
          disabled={converting}
        >
          <ArrowRight class="h-4 w-4 mr-1" />
          {converting ? 'Converting…' : 'Convert to Orders'}
        </Button>
      {/if}
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">
    {#if cartQuery.isPending || pendingBuildsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading cart…</div>
    {:else if cartQuery.isError}
      <div class="text-sm text-destructive">Error: {cartQuery.error.message}</div>
    {:else if isEmpty}
      <EmptyState
        title="Cart is empty"
        description="Create a build plan and submit it for fulfilment, or use Add Low Stock for replenishment."
      >
        {#snippet action()}
          <div class="flex gap-2">
            <Button variant="outline" size="sm" onclick={() => navigate(`/teams/${teamId}/builds`)}>
              View Builds
            </Button>
            <Button variant="outline" size="sm" onclick={handleAddLowStock} disabled={addingLowStock}>
              <RefreshCw class="h-4 w-4 mr-1" />
              {addingLowStock ? 'Adding…' : 'Add Low Stock'}
            </Button>
          </div>
        {/snippet}
      </EmptyState>
    {:else}
      <!-- Section 1: Pending Builds -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Pending Builds</h3>
        {#if pendingBuilds.length === 0}
          <p class="text-sm text-muted-foreground">
            No pending builds.
            <button class="underline" onclick={() => navigate(`/teams/${teamId}/builds`)}>Create a build plan</button>
            and submit for fulfilment to drive cart demand.
          </p>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead class="w-16">Include</TableHead>
                <TableHead>Project</TableHead>
                <TableHead>Revision</TableHead>
                <TableHead>Quantity</TableHead>
                <TableHead>Est. Order Cost</TableHead>
                <TableHead class="w-10"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each pendingBuilds as build (build.id)}
                {@const excluded = build.excluded_from_cart ?? false}
                {@const estCost = buildOrderCosts.get(build.id)}
                <TableRow>
                  <TableCell>
                    <input
                      type="checkbox"
                      checked={!excluded}
                      disabled={togglingBuilds.has(build.id)}
                      onchange={(e) => handleToggleInclusion(build.id, !(e.target as HTMLInputElement).checked)}
                      class="h-4 w-4 cursor-pointer"
                    />
                  </TableCell>
                  <TableCell class="text-sm font-medium">{build.project_name}</TableCell>
                  <TableCell class="font-mono text-xs text-muted-foreground">{build.bom_revision_label}</TableCell>
                  <TableCell class="tabular-nums">{build.quantity}</TableCell>
                  <TableCell class="tabular-nums text-sm">
                    {#if estCost !== undefined && estCost > 0}
                      <span title="Estimated parts ordering cost based on quantities and prices in the current cart">
                        <PriceDisplay value={estCost.toFixed(2)} />
                      </span>
                    {:else}
                      <span class="text-muted-foreground">—</span>
                    {/if}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onclick={() => navigate(`/teams/${teamId}/builds/${build.id}`)}
                      class="text-muted-foreground"
                    >
                      <ExternalLink class="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        {/if}
      </section>

      <!-- Section 2: Manual Requests (only when present) -->
      {#if manualItems.length > 0}
        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Manual Requests</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Part MPN</TableHead>
                <TableHead>Qty</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each manualItems as item (item.id)}
                <TableRow>
                  <TableCell class="font-mono font-medium text-xs">{item.part_mpn ?? '—'}</TableCell>
                  <TableCell class="tabular-nums">{item.quantity}</TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onclick={() => handleRemove(item.id)}
                      disabled={removingItems.has(item.id)}
                      class="text-destructive hover:text-destructive"
                    >
                      <Trash2 class="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        </section>
      {/if}

      <!-- Section 3: Order Summary -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Order Summary</h3>
        {#if (cartQuery.data?.items?.length ?? 0) === 0}
          <p class="text-sm text-muted-foreground">
            All BOM parts are covered by current inventory — nothing to order.
          </p>
        {:else}
          {#each [...distributorGroups.entries()] as [distributor, distItems]}
            {@const distTotal = distributorTotals.get(distributor)}
            {@const isUnassigned = distributor === 'Unassigned'}
            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <h4 class="text-sm font-medium">{distributor}</h4>
                  {#if isUnassigned}
                    <Badge variant="warning">No listing assigned</Badge>
                  {/if}
                </div>
                {#if distTotal && !isUnassigned}
                  <span class="text-sm text-muted-foreground">
                    Subtotal:
                    <span class="font-medium text-foreground"><PriceDisplay value={distTotal.subtotal} /></span>
                    {#if distTotal.fixed_order_cost !== '0.00'}
                      &nbsp;+ <PriceDisplay value={distTotal.fixed_order_cost} /> fixed
                    {/if}
                    &nbsp;= <span class="font-semibold text-foreground"><PriceDisplay value={distTotal.total} /></span>
                  </span>
                {/if}
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead class="w-6"></TableHead>
                    <TableHead>Part MPN</TableHead>
                    <TableHead>SKU</TableHead>
                    <TableHead>Order Qty</TableHead>
                    <TableHead>Unit Price</TableHead>
                    <TableHead>Subtotal</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {#each distItems as item (item.id)}
                    {@const moq = itemMoq(item)}
                    {@const belowMoq = moq > 0 && item.quantity < moq}
                    {@const demand = totalBuildDemand(item)}
                    {@const isExpanded = expandedItemId === item.id}
                    {@const orderQty = effectiveOrderQty(item)}
                    <!-- Main row -->
                    <TableRow
                      class="cursor-pointer hover:bg-muted/50 select-none"
                      onclick={() => toggleExpand(item.id)}
                    >
                      <TableCell class="text-muted-foreground pr-0">
                        {#if isExpanded}
                          <ChevronDown class="h-4 w-4" />
                        {:else}
                          <ChevronRight class="h-4 w-4" />
                        {/if}
                      </TableCell>
                      <TableCell class="font-mono font-medium text-xs">{item.part_mpn ?? '—'}</TableCell>
                      <TableCell class="text-muted-foreground text-sm">
                        {itemSku(item) ?? '—'}
                      </TableCell>
                      <TableCell onclick={(e: MouseEvent) => e.stopPropagation()}>
                        <div class="flex flex-col gap-0.5">
                          <div class="flex items-center gap-2">
                            <Input
                              type="number"
                              value={getQty(item)}
                              min="1"
                              class="w-20"
                              oninput={(e: Event) => handleQtyInput(item.id, (e.target as HTMLInputElement).value)}
                              onblur={() => commitQtyUpdate(item)}
                              onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') commitQtyUpdate(item); }}
                              disabled={updatingItems.has(item.id)}
                            />
                          </div>
                          <div class="flex gap-2 text-xs text-muted-foreground">
                            {#if demand > 0 && item.source === 'bom_shortfall'}
                              <span>Needs {demand}</span>
                            {/if}
                            {#if moq > 0}
                              <span class={belowMoq ? 'text-destructive font-medium' : ''}>MOQ {moq}</span>
                            {/if}
                            {#if orderQty > item.quantity}
                              <span class="text-emerald-600 dark:text-emerald-400">↑ {orderQty} (price break)</span>
                            {/if}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        {#if isUnassigned}
                          <span class="text-xs text-destructive">No listing</span>
                        {:else}
                          <PriceDisplay value={effectiveUnitPriceStr(item, orderQty)} decimals={4} />
                        {/if}
                      </TableCell>
                      <TableCell>
                        {#if isUnassigned}
                          <span class="text-muted-foreground">—</span>
                        {:else}
                          <PriceDisplay value={calcSubtotal(item, orderQty)} />
                        {/if}
                      </TableCell>
                      <TableCell onclick={(e: MouseEvent) => e.stopPropagation()}>
                        <Button
                          variant="ghost"
                          size="sm"
                          onclick={() => handleRemove(item.id)}
                          disabled={removingItems.has(item.id)}
                          class="text-destructive hover:text-destructive"
                        >
                          <Trash2 class="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>

                    <!-- Expandable detail row -->
                    {#if isExpanded}
                      {@const breaks = itemPriceBreaks(item)}
                      {@const activeTier = activePriceBreakQty(item, orderQty)}
                      {@const packaging = itemPackagingType(item)}
                      {@const stock = itemStockAvailable(item)}
                      {@const refreshed = itemLastRefreshed(item)}
                      {@const demands = item.build_demands ?? []}
                      <TableRow class="bg-muted/30 hover:bg-muted/30">
                        <TableCell colspan={7} class="py-3 px-4">
                          <div class="flex gap-10">

                            <!-- Card 1: Listing Details -->
                            <div class="space-y-2 shrink-0">
                              <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Listing Details</p>
                              <dl class="text-xs grid grid-cols-[5.5rem_auto] gap-x-4 gap-y-1">
                                <dt class="text-muted-foreground">Distributor</dt>
                                <dd class="font-medium">{itemDistributor(item)}</dd>
                                {#if itemSku(item)}
                                  <dt class="text-muted-foreground">SKU</dt>
                                  <dd class="font-mono">{itemSku(item)}</dd>
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
                                  {#if item.listing_id}
                                    <PriceDisplay value={item.fixed_order_cost} />
                                  {:else}
                                    <PriceDisplay value={item.resolved_listing?.fixed_order_cost} />
                                  {/if}
                                </dd>
                              </dl>
                            </div>

                            <!-- Card 2: Price Breaks -->
                            <div class="space-y-2 shrink-0">
                              <p class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Price Breaks</p>
                              {#if breaks.length === 0}
                                <p class="text-xs text-muted-foreground">
                                  {#if effectiveUnitPriceStr(item, orderQty)}
                                    Flat rate: <PriceDisplay value={effectiveUnitPriceStr(item, orderQty)} decimals={4} />
                                  {:else}
                                    No pricing data
                                  {/if}
                                </p>
                              {:else}
                                <table class="text-xs">
                                  <thead>
                                    <tr class="text-muted-foreground">
                                      <th class="text-left font-normal pb-1 pr-4">Min Qty</th>
                                      <th class="text-right font-normal pb-1 pr-4">Unit Price</th>
                                      <th class="text-right font-normal pb-1">@ {item.quantity}</th>
                                    </tr>
                                  </thead>
                                  <tbody>
                                    {#each breaks as pb}
                                      {@const isActive = pb.break_qty === activeTier}
                                      <tr class={isActive ? 'font-semibold text-foreground' : 'text-muted-foreground'}>
                                        <td class="py-0.5 pr-4">
                                          {#if isActive}<span class="mr-1">▶</span>{/if}{pb.break_qty.toLocaleString()}
                                        </td>
                                        <td class="text-right py-0.5 tabular-nums pr-4"><PriceDisplay value={pb.unit_price} decimals={4} /></td>
                                        <td class="text-right py-0.5 tabular-nums text-muted-foreground">
                                          {#if isActive}
                                            <PriceDisplay value={(parseFloat(pb.unit_price) * item.quantity).toFixed(2)} />
                                          {/if}
                                        </td>
                                      </tr>
                                    {/each}
                                  </tbody>
                                </table>
                              {/if}
                            </div>

                            <!-- Card 3: Build Demand -->
                            <div class="space-y-2 shrink-0">
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
                    {/if}

                  {/each}
                </TableBody>
              </Table>
            </div>
          {/each}
        {/if}
      </section>
    {/if}
  </div>
</div>

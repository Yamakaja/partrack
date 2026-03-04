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
  import { RefreshCw, ArrowRight, Trash2, ExternalLink } from 'lucide-svelte';
  import type { CartItem } from '$lib/types/orders.js';

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

  async function handleToggleInclusion(buildId: string, currentExcluded: boolean) {
    togglingBuilds = new Set([...togglingBuilds, buildId]);
    try {
      await toggleBuildCartInclusion(teamId, buildId, !currentExcluded);
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

  function effectiveUnitPrice(item: CartItem): number {
    if (!item.listing_id && item.resolved_listing?.unit_price) {
      return parseFloat(item.resolved_listing.unit_price);
    }
    let price = item.unit_price ? parseFloat(item.unit_price) : NaN;
    for (const pb of item.price_breaks ?? []) {
      if (item.quantity >= pb.break_qty) price = parseFloat(pb.unit_price);
    }
    return isNaN(price) ? 0 : price;
  }

  function effectiveUnitPriceStr(item: CartItem): string | null {
    const price = effectiveUnitPrice(item);
    return price > 0 ? price.toFixed(4) : null;
  }

  function calcSubtotal(item: CartItem): string | null {
    const hasPrice = item.unit_price || item.price_breaks?.length || item.resolved_listing?.unit_price;
    if (!hasPrice) return null;
    const price = effectiveUnitPrice(item);
    if (price === 0) return null;
    return (price * item.quantity).toFixed(2);
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
    if (item.listing_id) return 0;
    return item.resolved_listing?.moq ?? 0;
  }

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
        subtotal += effectiveUnitPrice(item) * item.quantity;
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
                <TableHead class="w-10"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each pendingBuilds as build (build.id)}
                {@const excluded = build.excluded_from_cart ?? false}
                <TableRow>
                  <TableCell>
                    <input
                      type="checkbox"
                      checked={!excluded}
                      disabled={togglingBuilds.has(build.id)}
                      onchange={() => handleToggleInclusion(build.id, excluded)}
                      class="h-4 w-4 cursor-pointer"
                    />
                  </TableCell>
                  <TableCell class="text-sm font-medium">{build.project_name}</TableCell>
                  <TableCell class="font-mono text-xs text-muted-foreground">{build.bom_revision_label}</TableCell>
                  <TableCell class="tabular-nums">{build.quantity}</TableCell>
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
                    <span class="font-medium text-foreground">${distTotal.subtotal}</span>
                    {#if distTotal.fixed_order_cost !== '0.00'}
                      &nbsp;+ ${distTotal.fixed_order_cost} fixed
                    {/if}
                    &nbsp;= <span class="font-semibold text-foreground">${distTotal.total}</span>
                  </span>
                {/if}
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Part MPN</TableHead>
                    <TableHead>Listing (SKU)</TableHead>
                    <TableHead>Qty</TableHead>
                    <TableHead>Unit Price</TableHead>
                    <TableHead>Subtotal</TableHead>
                    <TableHead></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {#each distItems as item (item.id)}
                    {@const moq = itemMoq(item)}
                    {@const belowMoq = moq > 0 && item.quantity < moq}
                    <TableRow>
                      <TableCell class="font-mono font-medium text-xs">{item.part_mpn ?? '—'}</TableCell>
                      <TableCell class="text-muted-foreground text-sm">
                        {itemSku(item) ?? '—'}
                      </TableCell>
                      <TableCell>
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
                          {#if belowMoq}
                            <span class="text-xs text-muted-foreground whitespace-nowrap">MOQ: {moq}</span>
                          {/if}
                        </div>
                      </TableCell>
                      <TableCell>
                        {#if isUnassigned}
                          <span class="text-xs text-destructive">No listing</span>
                        {:else}
                          <PriceDisplay value={effectiveUnitPriceStr(item)} />
                        {/if}
                      </TableCell>
                      <TableCell>
                        {#if isUnassigned}
                          <span class="text-muted-foreground">—</span>
                        {:else}
                          <PriceDisplay value={calcSubtotal(item)} />
                        {/if}
                      </TableCell>
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
            </div>
          {/each}
        {/if}
      </section>
    {/if}
  </div>
</div>

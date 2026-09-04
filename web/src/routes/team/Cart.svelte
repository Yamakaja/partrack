<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    getCart, addLowStock, convertCart, refreshCartListings,
  } from '$lib/api/cart.js';
  import { trackListingRefresh } from '$lib/api/batchJob.js';
  import { listBuilds } from '$lib/api/builds.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw, ArrowRight, SlidersHorizontal } from 'lucide-svelte';
  import CartPendingBuildsTable from '$lib/components/cart/CartPendingBuildsTable.svelte';
  import SourcingPolicyDialog from '$lib/components/cart/SourcingPolicyDialog.svelte';
  import CartManualRequestsTable from '$lib/components/cart/CartManualRequestsTable.svelte';
  import CartOrderSummaryTable from '$lib/components/cart/CartOrderSummaryTable.svelte';
  import CartOrderDetailsSection from '$lib/components/cart/CartOrderDetailsSection.svelte';
  import {
    effectiveOrderQty, effectiveUnitPrice, itemDistributor,
  } from '$lib/utils/cart.js';
  import type { CartItem } from '$lib/types/orders.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { hasMinRole } from '$lib/utils/roles.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  // Every role can see the cart; changing it is procurement (purchaser+).
  const canEdit = $derived(hasMinRole(activeTeamStore.current?.role ?? 'viewer', 'purchaser'));

  let cartQuery = createQuery(() => ({
    queryKey: ['cart', teamId],
    queryFn: () => getCart(teamId),
  }));

  let pendingBuildsQuery = createQuery(() => ({
    queryKey: ['builds', teamId, 'pending'],
    queryFn: () => listBuilds(teamId, { status: 'pending' }),
  }));

  let addingLowStock = $state(false);
  let converting = $state(false);
  let refreshingListings = $state(false);
  let sourcingPolicyOpen = $state(false);

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

  async function handleRefreshListings() {
    refreshingListings = true;
    try {
      const result = await refreshCartListings(teamId);
      if (!result || result.listing_count === 0) {
        pushToast('info', 'Nothing to refresh', 'No cart listings have a distributor integration.');
        return;
      }
      trackListingRefresh({
        teamId,
        jobId: result.job_id,
        total: result.listing_count,
        onDone: () => qc.invalidateQueries({ queryKey: ['cart', teamId] }),
      });
    } catch (err: any) {
      pushToast('error', 'Failed to queue refresh', err.message);
    } finally {
      refreshingListings = false;
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
      if (result.order_ids.length == 1) {
        navigate(`/teams/${teamId}/orders/${result.order_ids[0]}`);
      } else {
        navigate(`/teams/${teamId}/orders`);
      }

    } catch (err: any) {
      pushToast('error', 'Failed to convert cart', err.message);
    } finally {
      converting = false;
    }
  }

  // --- Derived ---

  let items = $derived(cartQuery.data?.items ?? []);

  // Part IDs that have at least one shortfall/maintenance item (the demand floor).
  let nonManualPartIds = $derived(
    new Set(items.filter(i => i.source !== 'manual').map(i => i.part_id))
  );

  // Part IDs where a manual item is a companion (excess) to a shortfall/maintenance item.
  let companionPartIds = $derived(
    new Set(
      items
        .filter(i => i.source === 'manual' && nonManualPartIds.has(i.part_id))
        .map(i => i.part_id)
    )
  );

  // Companion quantity per part_id (sum of manual companion items for the same part).
  let companionQtyByPartId = $derived.by(() => {
    const m = new Map<string, number>();
    for (const item of items) {
      if (item.source === 'manual' && companionPartIds.has(item.part_id)) {
        m.set(item.part_id, (m.get(item.part_id) ?? 0) + item.quantity);
      }
    }
    return m;
  });

  let distributorGroups = $derived.by(() => {
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
    for (const [dist, distItems] of distributorGroups) {
      let subtotal = 0;
      let fixedCost = 0;
      for (const item of distItems) {
        // Companion items are folded into their shortfall/maintenance sibling — skip here.
        if (item.source === 'manual' && companionPartIds.has(item.part_id)) continue;
        const compQty = companionQtyByPartId.get(item.part_id) ?? 0;
        const oq = effectiveOrderQty(item, compQty);
        subtotal += effectiveUnitPrice(item, oq) * oq;
        const fcStr = item.listing?.fixed_order_cost ?? '0';
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

  let buildOrderCosts = $derived.by(() => {
    const costs = new Map<string, number>();
    for (const item of items) {
      // Skip companion items — their cost is counted via the shortfall sibling.
      if (item.source === 'manual' && companionPartIds.has(item.part_id)) continue;
      const compQty = companionQtyByPartId.get(item.part_id) ?? 0;
      const unitPrice = effectiveUnitPrice(item, effectiveOrderQty(item, compQty));
      if (!unitPrice) continue;
      for (const d of item.build_demands ?? []) {
        costs.set(d.build_id, (costs.get(d.build_id) ?? 0) + d.quantity_needed * unitPrice);
      }
    }
    return costs;
  });

  let summaryGrandTotals = $derived.by(() => {
    let positions = 0;
    let parts = 0;
    let subtotal = 0;
    let total = 0;
    for (const [dist, distItems] of distributorGroups) {
      if (dist === 'Unassigned') continue;
      for (const item of distItems) {
        if (item.source === 'manual' && companionPartIds.has(item.part_id)) continue;
        positions++;
        const compQty = companionQtyByPartId.get(item.part_id) ?? 0;
        parts += effectiveOrderQty(item, compQty);
      }
      const t = distributorTotals.get(dist);
      if (t) {
        subtotal += parseFloat(t.subtotal);
        total += parseFloat(t.total);
      }
    }
    return { positions, parts, subtotal: subtotal.toFixed(2), total: total.toFixed(2) };
  });

  // Standalone manual items only — companions are shown via the shortfall row in Order Details.
  let manualItems = $derived(
    items.filter(i => i.source === 'manual' && !companionPartIds.has(i.part_id))
  );

  let pendingBuilds = $derived(pendingBuildsQuery.data?.items ?? []);
  let isEmpty = $derived(
    pendingBuilds.length === 0 && (cartQuery.data?.items?.length ?? 0) === 0
  );
</script>

<div class="flex flex-col h-full">
  <Topbar title="Shopping Cart">
    {#snippet actions()}
      {#if !canEdit}
        <span class="text-xs text-muted-foreground" title="Changing the cart requires the purchaser role">
          Read-only
        </span>
      {:else}
        <Button
          variant="outline"
          size="sm"
          onclick={() => (sourcingPolicyOpen = true)}
          title="Configure how cart items are matched to a distributor"
        >
          <SlidersHorizontal class="h-4 w-4 mr-1" />
          Sourcing Policy
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={handleAddLowStock}
          disabled={addingLowStock}
        >
          <RefreshCw class="h-4 w-4 mr-1" />
          {addingLowStock ? 'Adding…' : 'Add Low Stock'}
        </Button>
        {#if items.some(i => i.listing !== null)}
          <Button
            variant="outline"
            size="sm"
            onclick={handleRefreshListings}
            disabled={refreshingListings}
          >
            <RefreshCw class="h-4 w-4 mr-1" />
            {refreshingListings ? 'Queuing…' : 'Refresh Stock & Prices'}
          </Button>
        {/if}
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
            {#if canEdit}
              <Button variant="outline" size="sm" onclick={handleAddLowStock} disabled={addingLowStock}>
                <RefreshCw class="h-4 w-4 mr-1" />
                {addingLowStock ? 'Adding…' : 'Add Low Stock'}
              </Button>
            {/if}
          </div>
        {/snippet}
      </EmptyState>
    {:else}
      <!-- Section 1: Pending Builds -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Pending Builds</h3>
        <CartPendingBuildsTable {teamId} {pendingBuilds} {buildOrderCosts} {canEdit} />
      </section>

      <!-- Section 3: Order Summary + Details -->
      {#if (cartQuery.data?.items?.length ?? 0) === 0}
        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Order Summary</h3>
          <p class="text-sm text-muted-foreground">
            All BOM parts are covered by current inventory — nothing to order.
          </p>
        </section>
      {:else}
        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Order Summary</h3>
          <CartOrderSummaryTable {distributorGroups} {distributorTotals} {summaryGrandTotals} />
        </section>

        <section class="space-y-4">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Order Details</h3>
          <CartOrderDetailsSection {teamId} {distributorGroups} {companionPartIds} {canEdit} />
        </section>
      {/if}
    {/if}
  </div>
</div>

<SourcingPolicyDialog {teamId} bind:open={sourcingPolicyOpen} />

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getCart, updateCartItem, removeCartItem, addLowStock, convertCart } from '$lib/api/cart.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw, ArrowRight, Trash2 } from 'lucide-svelte';
  import type { CartItem } from '$lib/types/orders.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let cartQuery = createQuery(() => ({
    queryKey: ['cart', teamId],
    queryFn: () => getCart(teamId),
  }));

  let qtyEdits = $state<Record<string, string>>({});
  let updatingItems = $state<Set<string>>(new Set());
  let removingItems = $state<Set<string>>(new Set());
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
      pushToast('success', `Created ${result.order_ids.length} order(s)`);
      navigate(`/teams/${teamId}/orders`);
    } catch (err: any) {
      pushToast('error', 'Failed to convert cart', err.message);
    } finally {
      converting = false;
    }
  }

  function calcSubtotal(item: CartItem): string | null {
    if (!item.unit_price) return null;
    const total = parseFloat(item.unit_price) * item.quantity;
    return isNaN(total) ? null : total.toFixed(2);
  }

  let distributorGroups = $derived.by(() => {
    const items = cartQuery.data?.items ?? [];
    const groups = new Map<string, CartItem[]>();
    for (const item of items) {
      const key = item.listing_distributor_name ?? 'No Distributor';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push(item);
    }
    return groups;
  });
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
    {#if cartQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading cart…</div>
    {:else if cartQuery.isError}
      <div class="text-sm text-destructive">Error: {cartQuery.error.message}</div>
    {:else if cartQuery.data}
      {@const cart = cartQuery.data}
      {#if cart.items.length === 0}
        <EmptyState
          title="Cart is empty"
          description="Add low-stock items or shortfall items from a BOM stock check."
        >
          {#snippet action()}
            <Button variant="outline" size="sm" onclick={handleAddLowStock} disabled={addingLowStock}>
              <RefreshCw class="h-4 w-4 mr-1" />
              {addingLowStock ? 'Adding…' : 'Add Low Stock'}
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        {#each [...distributorGroups.entries()] as [distributor, items]}
          {@const distTotal = cart.distributor_totals.find(
            dt => dt.distributor_name === distributor || (!dt.distributor_name && distributor === 'No Distributor')
          )}
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <h2 class="text-sm font-semibold">{distributor}</h2>
              {#if distTotal}
                <span class="text-sm text-muted-foreground">
                  Subtotal:
                  <span class="font-medium text-foreground">${distTotal.subtotal}</span>
                  {#if distTotal.fixed_order_cost !== '0'}
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
                {#each items as item (item.id)}
                  <TableRow>
                    <TableCell class="font-mono font-medium text-xs">{item.part_mpn ?? '—'}</TableCell>
                    <TableCell class="text-muted-foreground text-sm">
                      {item.listing_sku ?? '—'}
                    </TableCell>
                    <TableCell>
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
                    </TableCell>
                    <TableCell>
                      <PriceDisplay value={item.unit_price} />
                    </TableCell>
                    <TableCell>
                      <PriceDisplay value={calcSubtotal(item)} />
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
    {/if}
  </div>
</div>

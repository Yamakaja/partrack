<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { updateCartItem, removeCartItem } from '$lib/api/cart.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ChevronDown, ChevronRight, SquareArrowOutUpRight, Trash2 } from 'lucide-svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import CartItemDetail from '$lib/components/cart/CartItemDetail.svelte';
  import {
    effectiveOrderQty, effectiveUnitPriceStr,
    calcSubtotal, itemSku, itemMoq, totalBuildDemand,
  } from '$lib/utils/cart.js';
  import type { CartItem } from '$lib/types/orders.js';
  import { navigate } from '$lib/router/index.svelte';

  let { teamId, distributorGroups, companionPartIds }: {
    teamId: string;
    distributorGroups: Map<string, CartItem[]>;
    companionPartIds: Set<string>;
  } = $props();

  // Map of part_id → manual companion quantity (excess above demand floor).
  const companionQtyByPartId = $derived.by(() => {
    const m = new Map<string, number>();
    for (const [, grpItems] of distributorGroups) {
      for (const item of grpItems) {
        if (item.source === 'manual' && companionPartIds.has(item.part_id)) {
          m.set(item.part_id, (m.get(item.part_id) ?? 0) + item.quantity);
        }
      }
    }
    return m;
  });

  const qc = useQueryClient();
  let expandedItemId = $state<string | null>(null);
  let qtyEdits = $state<Record<string, string>>({});
  let updatingItems = $state<Set<string>>(new Set());
  let removingItems = $state<Set<string>>(new Set());

  function toggleExpand(itemId: string) {
    expandedItemId = expandedItemId === itemId ? null : itemId;
  }

  function getQty(item: CartItem): string {
    if (qtyEdits[item.id] !== undefined) return qtyEdits[item.id];
    const companionQty = item.source !== 'manual' ? (companionQtyByPartId.get(item.part_id) ?? 0) : 0;
    return String(item.quantity + companionQty);
  }

  function handleQtyInput(itemId: string, val: string) {
    qtyEdits = { ...qtyEdits, [itemId]: val };
  }

  async function commitQtyUpdate(item: CartItem) {
    const raw = qtyEdits[item.id];
    if (raw === undefined) return;
    let effectiveQty = parseInt(raw, 10);
    const companionQty = item.source !== 'manual' ? (companionQtyByPartId.get(item.part_id) ?? 0) : 0;
    const combined = item.quantity + companionQty;
    if (isNaN(effectiveQty)) {
      const next = { ...qtyEdits };
      delete next[item.id];
      qtyEdits = next;
      return;
    }
    const minQty = item.source !== 'manual' ? item.quantity : 1;
    if (effectiveQty < minQty) effectiveQty = minQty;
    if (effectiveQty === combined) {
      const next = { ...qtyEdits };
      delete next[item.id];
      qtyEdits = next;
      return;
    }
    updatingItems = new Set([...updatingItems, item.id]);
    try {
      await updateCartItem(teamId, item.id, { quantity: effectiveQty });
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
</script>

{#each [...distributorGroups.entries()] as [distributor, distItems]}
  {@const isUnassigned = distributor === 'Unassigned'}
  <div class="space-y-2">
    <div class="flex items-center gap-2">
      <h4 class="text-sm font-medium">{distributor}</h4>
      {#if isUnassigned}
        <Badge variant="warning">No listing assigned</Badge>
      {/if}
    </div>
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead class="w-6"></TableHead>
          <TableHead>Part MPN</TableHead>
          <TableHead>Description</TableHead>
          <TableHead>SKU</TableHead>
          <TableHead>Order Qty</TableHead>
          <TableHead>Distributor Stock</TableHead>
          <TableHead>Unit Price</TableHead>
          <TableHead>Subtotal</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#each distItems.filter(i => !(i.source === 'manual' && companionPartIds.has(i.part_id))) as item (item.id)}
          {@const moq = itemMoq(item)}
          {@const companionQty = companionQtyByPartId.get(item.part_id) ?? 0}
          {@const combinedDemand = item.quantity + companionQty}
          {@const belowMoq = moq > 0 && combinedDemand < moq}
          {@const demand = totalBuildDemand(item)}
          {@const isExpanded = expandedItemId === item.id}
          {@const orderQty = effectiveOrderQty(item, companionQty)}
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
            <TableCell class="text-xs text-muted-foreground max-w-[14rem]">
              <span class="line-clamp-2 leading-snug" title={item.part_short_description ?? item.part_description ?? undefined}>
                {item.part_short_description ?? item.part_description ?? '—'}
              </span>
            </TableCell>
            <TableCell class="text-muted-foreground text-sm">
              {itemSku(item) ?? '—'}
            </TableCell>
            <TableCell>
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
                    onclick={(e: MouseEvent) => e.stopPropagation()}
                    disabled={updatingItems.has(item.id)}
                  />
                </div>
                <div class="flex gap-2 text-xs text-muted-foreground">
                  {#if demand > 0 && item.source === 'bom_shortfall'}
                    <span>Needs {demand}</span>
                  {/if}
                  {#if companionQty > 0}
                    <span>+{companionQty} manual</span>
                  {/if}
                  {#if moq > 0}
                    <span class={belowMoq ? 'text-destructive font-medium' : ''}>MOQ {moq}</span>
                  {/if}
                  {#if orderQty > combinedDemand}
                    <span class="text-success">↑ {orderQty} (price break)</span>
                  {/if}
                </div>
              </div>
            </TableCell>
            <TableCell class="tabular-nums text-sm">
              {@const stock = item.listing?.stock_available ?? null}
              {#if stock === null}
                <span class="text-muted-foreground">—</span>
              {:else if stock === 0}
                <span class="text-destructive font-medium">0</span>
              {:else}
                <span class={stock < orderQty ? 'text-warning' : ''}>{stock.toLocaleString()}</span>
              {/if}
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
              <Button
                variant="ghost"
                size="sm"
                onclick={() => navigate(`/teams/${teamId}/parts/${item.part_id}`)}
              >
                <SquareArrowOutUpRight class="h-4 w-4"/>
              </Button>
            </TableCell>

          </TableRow>

          <!-- Expandable detail row -->
          {#if isExpanded}
            <CartItemDetail {item} {orderQty} {moq} {demand} {companionQty} />
          {/if}

        {/each}
      </TableBody>
    </Table>
  </div>
{/each}

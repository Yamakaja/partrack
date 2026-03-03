<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listCheckouts, createCheckout, type InsufficientCheckoutItem } from '$lib/api/checkouts.js';
  import { ApiError } from '$lib/api/client.js';
  import { listInventory } from '$lib/api/inventory.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import Pagination from '$lib/components/common/Pagination.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus, Trash2 } from 'lucide-svelte';
  import type { InventoryEntry } from '$lib/types/inventory.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let limit = $state(50);
  let offset = $state(0);

  let checkoutsQuery = createQuery(() => ({
    queryKey: ['checkouts', teamId, limit, offset],
    queryFn: () => listCheckouts(teamId, { limit, offset }),
  }));

  // Dialog state
  let dialogOpen = $state(false);
  let checkoutReason = $state('');
  let submitting = $state(false);

  // Reconciliation state
  let reconcileOpen = $state(false);
  let insufficientItems = $state<InsufficientCheckoutItem[]>([]);
  // Maps inventory_entry_id → user-supplied actual count string
  let reconcileCounts = $state<Record<string, string>>({});

  interface CheckoutLineItem {
    id: number;
    inventory_entry_id: string;
    part_mpn: string | null;
    quantity: string;
    inventorySearch: string;
    searchResults: InventoryEntry[];
    searching: boolean;
    searchOpen: boolean;
  }

  let lineItems = $state<CheckoutLineItem[]>([]);
  let nextId = $state(1);

  let inventorySearchTimers: Record<number, ReturnType<typeof setTimeout>> = {};

  function openDialog() {
    checkoutReason = '';
    lineItems = [newLineItem()];
    dialogOpen = true;
  }

  function newLineItem(): CheckoutLineItem {
    const id = nextId++;
    return {
      id,
      inventory_entry_id: '',
      part_mpn: null,
      quantity: '1',
      inventorySearch: '',
      searchResults: [],
      searching: false,
      searchOpen: false,
    };
  }

  function addLineItem() {
    lineItems = [...lineItems, newLineItem()];
  }

  function removeLineItem(id: number) {
    lineItems = lineItems.filter(li => li.id !== id);
  }

  async function searchInventory(item: CheckoutLineItem, q: string) {
    if (!q || q.length < 2) {
      lineItems = lineItems.map(li => li.id === item.id ? { ...li, searchResults: [], searchOpen: false } : li);
      return;
    }
    lineItems = lineItems.map(li => li.id === item.id ? { ...li, searching: true } : li);
    try {
      const result = await listInventory(teamId, { q, limit: 10 });
      lineItems = lineItems.map(li =>
        li.id === item.id
          ? { ...li, searchResults: result.items, searchOpen: result.items.length > 0, searching: false }
          : li
      );
    } catch {
      lineItems = lineItems.map(li => li.id === item.id ? { ...li, searching: false } : li);
    }
  }

  function handleInventoryInput(item: CheckoutLineItem, q: string) {
    lineItems = lineItems.map(li => li.id === item.id ? { ...li, inventorySearch: q, searchOpen: false } : li);
    clearTimeout(inventorySearchTimers[item.id]);
    inventorySearchTimers[item.id] = setTimeout(() => {
      const updated = lineItems.find(li => li.id === item.id);
      if (updated) searchInventory(updated, q);
    }, 300);
  }

  function selectInventoryEntry(itemId: number, entry: InventoryEntry) {
    lineItems = lineItems.map(li =>
      li.id === itemId
        ? {
            ...li,
            inventory_entry_id: entry.id,
            part_mpn: entry.part_mpn,
            inventorySearch: entry.part_mpn ?? entry.id,
            searchResults: [],
            searchOpen: false,
          }
        : li
    );
  }

  async function submitCheckout(reconcileMap?: Record<string, number>) {
    const items = lineItems.filter(li => li.inventory_entry_id && parseInt(li.quantity, 10) > 0);
    if (items.length === 0) {
      pushToast('error', 'Add at least one item to check out');
      return;
    }
    submitting = true;
    try {
      await createCheckout(teamId, {
        reason: checkoutReason.trim() || undefined,
        items: items.map(li => ({
          inventory_entry_id: li.inventory_entry_id,
          quantity: parseInt(li.quantity, 10),
          ...(reconcileMap?.[li.inventory_entry_id] !== undefined
            ? { reconcile_to: reconcileMap[li.inventory_entry_id] }
            : {}),
        })),
      });
      dialogOpen = false;
      reconcileOpen = false;
      qc.invalidateQueries({ queryKey: ['checkouts', teamId] });
      qc.invalidateQueries({ queryKey: ['inventory', teamId] });
      pushToast('success', 'Checkout recorded');
    } catch (err: any) {
      if (err instanceof ApiError && err.status === 409 && err.code === 'INSUFFICIENT_STOCK') {
        const items = (err.data as any).insufficient_items as InsufficientCheckoutItem[];
        insufficientItems = items;
        reconcileCounts = Object.fromEntries(
          items.map(i => [i.inventory_entry_id, String(i.quantity_requested)])
        );
        dialogOpen = false;
        reconcileOpen = true;
      } else {
        pushToast('error', 'Failed to create checkout', err.message);
      }
    } finally {
      submitting = false;
    }
  }

  async function handleSubmit(e: Event) {
    e.preventDefault();
    await submitCheckout();
  }

  async function handleReconcileSubmit(e: Event) {
    e.preventDefault();
    const reconcileMap: Record<string, number> = {};
    for (const item of insufficientItems) {
      const val = parseInt(reconcileCounts[item.inventory_entry_id], 10);
      if (isNaN(val) || val < item.quantity_requested) {
        pushToast('error', `Actual count for ${item.part_mpn ?? item.part_id} must be at least ${item.quantity_requested}`);
        return;
      }
      reconcileMap[item.inventory_entry_id] = val;
    }
    await submitCheckout(reconcileMap);
  }

  function shortId(id: string): string {
    return id.slice(0, 8);
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Checkouts">
    {#snippet actions()}
      <Button size="sm" onclick={openDialog}>
        <Plus class="h-4 w-4 mr-1" /> New Checkout
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    {#if checkoutsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading checkouts…</div>
    {:else if checkoutsQuery.isError}
      <div class="text-sm text-destructive">Error: {checkoutsQuery.error.message}</div>
    {:else if checkoutsQuery.data}
      {@const data = checkoutsQuery.data}
      {#if data.items.length === 0}
        <EmptyState
          title="No checkouts yet"
          description="Record ad-hoc parts withdrawals not tied to a build."
        >
          {#snippet action()}
            <Button size="sm" onclick={openDialog}>
              <Plus class="h-4 w-4 mr-1" /> New Checkout
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Created By</TableHead>
              <TableHead># Items</TableHead>
              <TableHead>Checked Out At</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each data.items as checkout (checkout.id)}
              <TableRow class="cursor-pointer" onclick={() => navigate(`/teams/${teamId}/checkouts/${checkout.id}`)}>
                <TableCell class="font-mono text-xs text-muted-foreground">{shortId(checkout.id)}</TableCell>
                <TableCell class="font-medium">{checkout.created_by_display_name}</TableCell>
                <TableCell class="text-muted-foreground">{checkout.items?.length ?? 0}</TableCell>
                <TableCell>
                  <DateDisplay value={checkout.checked_out_at} />
                </TableCell>
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
</div>

<!-- Reconciliation Dialog -->
<Dialog.Root bind:open={reconcileOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-2">Stock Count Mismatch</Dialog.Title>
      <p class="text-sm text-muted-foreground mb-4">
        The system shows less stock than you're trying to check out. Enter the actual count you physically observe for each item. The records will be corrected before the checkout is applied.
      </p>
      <form onsubmit={handleReconcileSubmit} class="space-y-4">
        {#each insufficientItems as item (item.inventory_entry_id)}
          <div class="space-y-1">
            <Label>
              {item.part_mpn ?? item.part_id}
              <span class="text-muted-foreground text-xs ml-1">(system shows {item.quantity_on_hand}, need {item.quantity_requested})</span>
            </Label>
            <Input
              type="number"
              min={item.quantity_requested}
              value={reconcileCounts[item.inventory_entry_id]}
              oninput={(e: Event) => {
                reconcileCounts = { ...reconcileCounts, [item.inventory_entry_id]: (e.target as HTMLInputElement).value };
              }}
              placeholder={`Actual count (min ${item.quantity_requested})`}
            />
          </div>
        {/each}
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { reconcileOpen = false; dialogOpen = true; }}>Back</Button>
          <Button type="submit" disabled={submitting}>
            {submitting ? 'Saving…' : 'Confirm & Submit'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- New Checkout Dialog -->
<Dialog.Root bind:open={dialogOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-xl -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">New Checkout</Dialog.Title>
      <form onsubmit={handleSubmit} class="space-y-4">
        <div class="space-y-1">
          <Label for="checkout-reason">Reason</Label>
          <Input id="checkout-reason" bind:value={checkoutReason} placeholder="Optional reason" />
        </div>

        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <Label>Items <span class="text-destructive">*</span></Label>
            <Button type="button" variant="outline" size="sm" onclick={addLineItem}>
              <Plus class="h-3.5 w-3.5 mr-1" /> Add Item
            </Button>
          </div>
          {#each lineItems as item (item.id)}
            <div class="flex items-start gap-2">
              <div class="flex-1 relative">
                <input
                  type="text"
                  value={item.inventorySearch}
                  oninput={(e: Event) => handleInventoryInput(item, (e.target as HTMLInputElement).value)}
                  onblur={() => setTimeout(() => {
                    lineItems = lineItems.map(li => li.id === item.id ? { ...li, searchOpen: false } : li);
                  }, 150)}
                  placeholder="Search by MPN…"
                  class="flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
                {#if item.searchOpen && item.searchResults.length > 0}
                  <ul class="absolute z-50 mt-1 w-full rounded-md border border-border bg-popover shadow-md max-h-48 overflow-y-auto">
                    {#each item.searchResults as entry (entry.id)}
                      <li>
                        <button
                          type="button"
                          class="w-full px-3 py-2 text-left text-sm hover:bg-accent"
                          onmousedown={() => selectInventoryEntry(item.id, entry)}
                        >
                          <span class="font-mono font-medium">{entry.part_mpn ?? '—'}</span>
                          <span class="ml-2 text-muted-foreground text-xs">
                            {entry.part_description ?? ''}
                            &nbsp;(on hand: {entry.quantity_on_hand})
                          </span>
                        </button>
                      </li>
                    {/each}
                  </ul>
                {/if}
              </div>
              <Input
                type="number"
                value={item.quantity}
                min="1"
                class="w-20"
                oninput={(e: Event) => {
                  const val = (e.target as HTMLInputElement).value;
                  lineItems = lineItems.map(li => li.id === item.id ? { ...li, quantity: val } : li);
                }}
                placeholder="Qty"
              />
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onclick={() => removeLineItem(item.id)}
                disabled={lineItems.length <= 1}
                class="text-destructive hover:text-destructive"
              >
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
          {/each}
        </div>

        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { dialogOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={submitting}>
            {submitting ? 'Saving…' : 'Record Checkout'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

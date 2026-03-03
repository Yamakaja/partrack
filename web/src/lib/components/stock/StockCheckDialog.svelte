<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { getStockCheck } from '$lib/api/revisions.js';
  import { addShortfall } from '$lib/api/cart.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import type { StockCheckResult, StockCheckItem } from '$lib/types/projects.js';
  import { ShoppingCart, Zap, Loader2 } from 'lucide-svelte';

  let {
    teamId,
    projectId,
    revId,
    trigger,
  }: {
    teamId: string;
    projectId: string;
    revId: string;
    trigger: import('svelte').Snippet<[() => void]>;
  } = $props();

  let qtyOpen = $state(false);
  let resultsOpen = $state(false);
  let buildQty = $state(1);
  let checking = $state(false);
  let addingToCart = $state(false);
  let result = $state<StockCheckResult | null>(null);

  function openQtyDialog() {
    qtyOpen = true;
  }

  async function handleCheck() {
    if (buildQty < 1) return;
    checking = true;
    try {
      result = await getStockCheck(teamId, projectId, revId, buildQty);
      qtyOpen = false;
      resultsOpen = true;
    } catch (err: any) {
      pushToast('error', 'Stock check failed', err.message);
    } finally {
      checking = false;
    }
  }

  function handleBack() {
    result = null;
    resultsOpen = false;
    qtyOpen = true;
  }

  async function handleAddToCart() {
    if (!result) return;
    addingToCart = true;
    try {
      const res = await addShortfall(teamId, revId, buildQty);
      pushToast('success', `${res.added_count} item(s) added to cart`);
      resultsOpen = false;
    } catch (err: any) {
      pushToast('error', 'Failed to add to cart', err.message);
    } finally {
      addingToCart = false;
    }
  }

  function stockStatusVariant(item: StockCheckItem): 'success' | 'warning' | 'destructive' {
    if (item.stock_status === 'out_of_stock') return 'destructive';
    if (item.stock_status === 'partial') return 'warning';
    if (item.below_restock) return 'warning';
    return 'success';
  }

  function stockStatusLabel(item: StockCheckItem): string {
    if (item.stock_status === 'out_of_stock') return 'Out of Stock';
    if (item.stock_status === 'partial') return 'Partial';
    if (item.below_restock) return 'Below Restock';
    return 'In Stock';
  }

  let actionItems = $derived(
    result?.items.filter((i) => i.shortfall > 0 || i.below_restock) ?? []
  );

  let shortfallCount = $derived(actionItems.length);
</script>

{@render trigger(openQtyDialog)}

<!-- Dialog 1: Build quantity input -->
<Dialog.Root bind:open={qtyOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Check Stock</Dialog.Title>
      <Dialog.Description class="text-sm text-muted-foreground mb-5">
        Enter a build quantity to check parts availability.
      </Dialog.Description>

      <div class="space-y-1 mb-6">
        <Label for="build-qty">Build Quantity</Label>
        <Input
          id="build-qty"
          type="number"
          bind:value={buildQty}
          min="1"
          class="w-full"
          onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') handleCheck(); }}
        />
      </div>

      <div class="flex justify-end gap-2">
        <Button variant="outline" onclick={() => (qtyOpen = false)}>Cancel</Button>
        <Button onclick={handleCheck} disabled={checking || buildQty < 1}>
          {#if checking}
            <Loader2 class="h-4 w-4 animate-spin mr-1" /> Checking…
          {:else}
            <Zap class="h-4 w-4 mr-1" /> Check Stock
          {/if}
        </Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Dialog 2: Results -->
<Dialog.Root bind:open={resultsOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex w-[92vw] max-w-4xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
      style="max-height: min(85vh, 720px);"
    >
      <!-- Header -->
      <div class="shrink-0 border-b border-border px-6 pb-4 pt-5">
        <Dialog.Title class="text-lg font-semibold mb-3">Stock Check Results</Dialog.Title>

        {#if result}
          <!-- Summary row -->
          <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            <span>
              Build qty: <span class="font-semibold">{result.build_quantity}</span>
            </span>
            <span class="text-muted-foreground">·</span>
            <span>
              Fully buildable: <span class="font-semibold">{result.fully_buildable}</span>
            </span>
            <span class="text-muted-foreground">·</span>
            {#if shortfallCount > 0}
              <span class="font-medium text-destructive">{shortfallCount} item(s) need attention</span>
            {:else}
              <span class="font-medium text-green-600">All parts covered</span>
            {/if}
          </div>
        {/if}
      </div>

      <!-- Scrollable table -->
      <div class="min-h-0 flex-1 overflow-auto">
        {#if result}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Part MPN</TableHead>
                <TableHead>Ref Des</TableHead>
                <TableHead class="text-right">Qty/Board</TableHead>
                <TableHead class="text-right">Required</TableHead>
                <TableHead class="text-right">On Hand</TableHead>
                <TableHead class="text-right">Shortfall</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each result.items as item (item.bom_line_item_id)}
                <TableRow>
                  <TableCell class="font-mono text-xs font-medium">
                    {item.resolved_part_mpn ?? '—'}
                  </TableCell>
                  <TableCell class="text-xs text-muted-foreground max-w-[180px] truncate">
                    {item.reference_designators}
                  </TableCell>
                  <TableCell class="text-right tabular-nums">{item.quantity_per_board}</TableCell>
                  <TableCell class="text-right tabular-nums">{item.required_quantity}</TableCell>
                  <TableCell class="text-right tabular-nums">{item.quantity_on_hand}</TableCell>
                  <TableCell class="text-right tabular-nums">
                    {#if item.shortfall > 0}
                      <span class="font-semibold text-destructive">{item.shortfall}</span>
                    {:else}
                      <span class="text-muted-foreground">—</span>
                    {/if}
                  </TableCell>
                  <TableCell>
                    <Badge variant={stockStatusVariant(item)}>
                      {stockStatusLabel(item)}
                    </Badge>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        {/if}
      </div>

      <!-- Footer -->
      <div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
        <Button variant="outline" onclick={handleBack}>← Back</Button>
        <div class="flex gap-2">
          <Button variant="outline" onclick={() => (resultsOpen = false)}>Close</Button>
          <Button
            onclick={handleAddToCart}
            disabled={addingToCart || actionItems.length === 0}
          >
            <ShoppingCart class="h-4 w-4 mr-1" />
            {addingToCart ? 'Adding…' : `Add to Cart${actionItems.length > 0 ? ` (${actionItems.length})` : ''}`}
          </Button>
        </div>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

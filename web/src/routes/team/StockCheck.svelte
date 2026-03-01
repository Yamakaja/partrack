<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { getProject } from '$lib/api/projects.js';
  import { getRevision, getStockCheck } from '$lib/api/revisions.js';
  import { addShortfall } from '$lib/api/cart.js';
  import type { StockCheckResult, StockCheckItem } from '$lib/types/projects.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ShoppingCart, Zap } from 'lucide-svelte';

  let { teamId, projectId, revId }: { teamId: string; projectId: string; revId: string } = $props();

  let buildQty = $state(1);
  let result = $state<StockCheckResult | null>(null);
  let checking = $state(false);
  let addingAll = $state(false);
  let addingItemId = $state<string | null>(null);

  let projectQuery = createQuery(() => ({
    queryKey: ['project', teamId, projectId],
    queryFn: () => getProject(teamId, projectId),
  }));

  let revisionQuery = createQuery(() => ({
    queryKey: ['revision-meta', teamId, projectId, revId],
    queryFn: () => getRevision(teamId, projectId, revId),
  }));

  let projectName = $derived(projectQuery.data?.name ?? 'Project');
  let revisionLabel = $derived(revisionQuery.data?.label ?? 'Revision');

  async function handleCheck() {
    if (buildQty < 1) return;
    checking = true;
    try {
      result = await getStockCheck(teamId, projectId, revId, buildQty);
    } catch (err: any) {
      pushToast('error', 'Stock check failed', err.message);
    } finally {
      checking = false;
    }
  }

  async function handleAddToCart(item: StockCheckItem) {
    if (!item.resolved_part_id || item.shortfall <= 0) return;
    addingItemId = item.bom_line_item_id;
    try {
      // Add shortfall for the whole revision; the API doesn't support per-item cart add
      // so we use the full revision shortfall endpoint here with current build qty
      await addShortfall(teamId, revId, buildQty);
      pushToast('success', 'Shortfall added to cart');
    } catch (err: any) {
      pushToast('error', 'Failed to add to cart', err.message);
    } finally {
      addingItemId = null;
    }
  }

  async function handleAddAllShortfalls() {
    if (!result) return;
    const hasShortfall = result.items.some((i) => i.shortfall > 0);
    if (!hasShortfall) {
      pushToast('info', 'No shortfalls to add');
      return;
    }
    addingAll = true;
    try {
      const res = await addShortfall(teamId, revId, buildQty);
      pushToast('success', `${res.added_count} item(s) added to cart`);
    } catch (err: any) {
      pushToast('error', 'Failed to add shortfalls to cart', err.message);
    } finally {
      addingAll = false;
    }
  }

  function stockStatusVariant(status: StockCheckItem['stock_status']): 'success' | 'warning' | 'destructive' {
    if (status === 'in_stock') return 'success';
    if (status === 'partial') return 'warning';
    return 'destructive';
  }

  function stockStatusLabel(status: StockCheckItem['stock_status']): string {
    if (status === 'in_stock') return 'In Stock';
    if (status === 'partial') return 'Partial';
    return 'Out of Stock';
  }

  let shortfallItems = $derived(result?.items.filter((i) => i.shortfall > 0) ?? []);
</script>

<div class="flex flex-col h-full">
  <Topbar
    title="Stock Check"
    breadcrumbs={[
      { label: 'Projects', href: `/teams/${teamId}/projects` },
      { label: projectName, href: `/teams/${teamId}/projects/${projectId}` },
      { label: revisionLabel, href: `/teams/${teamId}/projects/${projectId}/revisions/${revId}` },
    ]}
  >
    {#snippet actions()}
      {#if result && shortfallItems.length > 0}
        <Button
          size="sm"
          variant="outline"
          onclick={handleAddAllShortfalls}
          disabled={addingAll}
        >
          <ShoppingCart class="h-4 w-4 mr-1" />
          {addingAll ? 'Adding…' : `Add All Shortfalls (${shortfallItems.length})`}
        </Button>
      {/if}
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-6">
    <!-- Build qty input + run button -->
    <div class="flex items-end gap-4">
      <div class="space-y-1">
        <Label for="build-qty">Build Quantity</Label>
        <Input
          id="build-qty"
          type="number"
          bind:value={buildQty}
          class="w-28"
          min="1"
        />
      </div>
      <Button onclick={handleCheck} disabled={checking || buildQty < 1}>
        <Zap class="h-4 w-4 mr-1" />
        {checking ? 'Checking…' : 'Check Stock'}
      </Button>
    </div>

    <!-- Summary banner -->
    {#if result}
      <div class="rounded-md border border-border bg-muted/50 px-4 py-3 text-sm">
        <span class="font-medium">Build Quantity: {result.build_quantity}</span>
        <span class="mx-3 text-muted-foreground">·</span>
        <span class="text-muted-foreground">
          Can fully build: <span class="font-medium text-foreground">{result.fully_buildable}</span>
        </span>
        {#if shortfallItems.length > 0}
          <span class="mx-3 text-muted-foreground">·</span>
          <span class="text-destructive font-medium">{shortfallItems.length} shortfall(s)</span>
        {:else}
          <span class="mx-3 text-muted-foreground">·</span>
          <span class="text-green-600 font-medium">All parts in stock</span>
        {/if}
      </div>

      <!-- Results table -->
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Part MPN</TableHead>
            <TableHead>Ref Des</TableHead>
            <TableHead class="text-right">Req'd/Board</TableHead>
            <TableHead class="text-right">Required</TableHead>
            <TableHead class="text-right">On Hand</TableHead>
            <TableHead class="text-right">Shortfall</TableHead>
            <TableHead>Status</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each result.items as item (item.bom_line_item_id)}
            <TableRow>
              <TableCell class="font-mono text-xs font-medium">
                {item.resolved_part_mpn ?? '—'}
              </TableCell>
              <TableCell class="text-xs text-muted-foreground">
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
                <Badge variant={stockStatusVariant(item.stock_status)}>
                  {stockStatusLabel(item.stock_status)}
                </Badge>
              </TableCell>
              <TableCell>
                {#if item.shortfall > 0 && item.resolved_part_id}
                  <Button
                    variant="ghost"
                    size="sm"
                    onclick={() => handleAddToCart(item)}
                    disabled={addingItemId === item.bom_line_item_id}
                  >
                    <ShoppingCart class="h-3.5 w-3.5 mr-1" />
                    Add
                  </Button>
                {/if}
              </TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
    {/if}
  </div>
</div>

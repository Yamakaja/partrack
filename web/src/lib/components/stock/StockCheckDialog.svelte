<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import PartRef from '$lib/components/parts/PartRef.svelte';
  import { getStockCheck } from '$lib/api/revisions.js';
  import { createBuild, submitBuild, checkoutBuild } from '$lib/api/builds.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import type { StockCheckResult, StockCheckItem } from '$lib/types/projects.js';
  import { Zap, Loader2, CheckCircle, Send } from 'lucide-svelte';

  let {
    teamId,
    projectId,
    revId,
    trigger,
  }: {
    teamId: string;
    projectId: string;
    revId: string;
    trigger: import('svelte').Snippet<[(e: UIEvent) => void]>;
  } = $props();

  let qtyOpen = $state(false);
  let resultsOpen = $state(false);
  let buildQty = $state(1);
  let checking = $state(false);
  let result = $state<StockCheckResult | null>(null);

  // Build creation state
  type BuildAction = 'idle' | 'creating' | 'created' | 'submitting' | 'checking_out';
  let buildAction = $state<BuildAction>('idle');
  let createdBuildId = $state<string | null>(null);
  let hasShortfall = $state(false);

  function openQtyDialog(e: UIEvent) {
    qtyOpen = true;
    e.stopPropagation();
  }

  async function handleCheck(e: UIEvent) {
    if (buildQty < 1) return;
    checking = true;
    try {
      result = await getStockCheck(teamId, projectId, revId, buildQty);
      qtyOpen = false;
      resultsOpen = true;
      buildAction = 'idle';
      createdBuildId = null;
      e.stopPropagation();
    } catch (err: any) {
      pushToast('error', 'Stock check failed', err.message);
    } finally {
      checking = false;
    }
  }

  function handleBack() {
    result = null;
    resultsOpen = false;
    buildAction = 'idle';
    createdBuildId = null;
    qtyOpen = true;
  }

  async function handleCreateBuild() {
    if (!result) return;
    buildAction = 'creating';
    hasShortfall = !result.fully_buildable;
    try {
      const build = await createBuild(teamId, {
        project_id: projectId,
        bom_revision_id: revId,
        quantity: buildQty,
      });
      createdBuildId = build.id;
      buildAction = 'created';
    } catch (err: any) {
      pushToast('error', 'Failed to create build plan', err.message);
      buildAction = 'idle';
    }
  }

  async function handleCheckoutNow() {
    if (!createdBuildId) return;
    buildAction = 'checking_out';
    try {
      await checkoutBuild(teamId, createdBuildId, {});
      pushToast('success', 'Build checked out — assembly recorded');
      resultsOpen = false;
    } catch (err: any) {
      pushToast('error', 'Checkout failed — open the build to retry', err.message);
      buildAction = 'created';
    }
  }

  async function handleSubmitForFulfilment() {
    if (!createdBuildId) return;
    buildAction = 'submitting';
    try {
      await submitBuild(teamId, createdBuildId);
      pushToast('success', 'Build submitted — shortfalls added to cart');
      resultsOpen = false;
    } catch (err: any) {
      pushToast('error', 'Failed to submit build', err.message);
      buildAction = 'created';
    }
  }

  function viewBuild() {
    if (createdBuildId) {
      resultsOpen = false;
      navigate(`/teams/${teamId}/builds/${createdBuildId}`);
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

  let shortfallCount = $derived(
    result?.items.filter((i) => i.shortfall > 0 || i.below_restock).length ?? 0
  );
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
          onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') handleCheck(e); }}
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
      class="fixed left-1/2 top-1/2 z-50 flex w-[92vw] max-w-5xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
      style="max-height: min(85vh, 720px);"
    >
      <!-- Header -->
      <div class="shrink-0 border-b border-border px-6 pb-4 pt-5">
        <Dialog.Title class="text-lg font-semibold mb-3">Stock Check Results</Dialog.Title>

        {#if result}
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
              <span class="font-medium text-success">All parts covered</span>
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
                <TableHead class="text-right">Free</TableHead>
                <TableHead class="text-right">On Order</TableHead>
                <TableHead class="text-right">Shortfall</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each result.items as item (item.bom_line_item_id)}
                {@const usingSub = !!item.selected_substitute_id}
                {@const displayQty = usingSub ? (item.substitute_qty_on_hand ?? item.quantity_on_hand) : item.quantity_on_hand}
                <TableRow class={usingSub ? 'bg-info-subtle' : ''}>
                  <TableCell class="font-mono text-xs font-medium">
                    <div class="flex items-center gap-1.5">
                      {#if usingSub}
                        <span class="text-muted-foreground line-through">{item.resolved_part_mpn ?? '—'}</span>
                        <PartRef partId={item.selected_substitute_id!} mpn={item.selected_substitute_mpn} {teamId} />
                        <Badge variant="secondary" class="text-[10px] px-1 py-0">sub</Badge>
                      {:else if item.resolved_part_id}
                        <PartRef partId={item.resolved_part_id} mpn={item.resolved_part_mpn} {teamId} />
                      {:else}
                        —
                      {/if}
                    </div>
                  </TableCell>
                  <TableCell class="text-xs text-muted-foreground max-w-[180px] truncate">
                    {item.reference_designators}
                  </TableCell>
                  <TableCell class="text-right tabular-nums">{item.quantity_per_board}</TableCell>
                  <TableCell class="text-right tabular-nums">{item.required_quantity}</TableCell>
                  <TableCell class="text-right tabular-nums text-muted-foreground">{displayQty}</TableCell>
                  <TableCell class="text-right tabular-nums">
                    {#if item.quantity_reserved > 0}
                      <span title="{item.quantity_reserved} reserved by active builds">{item.quantity_free}</span>
                    {:else}
                      {item.quantity_free}
                    {/if}
                  </TableCell>
                  <TableCell class="text-right tabular-nums text-muted-foreground">
                    {#if item.quantity_on_order > 0}
                      <span title="May be partially allocated to other builds">{item.quantity_on_order}</span>
                    {:else}
                      <span class="text-muted-foreground/40">—</span>
                    {/if}
                  </TableCell>
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
      <div class="shrink-0 border-t border-border px-6 py-4">
        {#if buildAction === 'idle'}
          <div class="flex items-center justify-between">
            <Button variant="outline" onclick={handleBack}>← Back</Button>
            <div class="flex gap-2">
              <Button variant="outline" onclick={() => (resultsOpen = false)}>Close</Button>
              <Button onclick={handleCreateBuild} disabled={!result}>
                Create Build Plan
              </Button>
            </div>
          </div>

        {:else if buildAction === 'creating'}
          <div class="flex items-center justify-between">
            <Button variant="outline" onclick={handleBack}>← Back</Button>
            <Button disabled>
              <Loader2 class="h-4 w-4 animate-spin mr-1" /> Creating…
            </Button>
          </div>

        {:else if buildAction === 'created'}
          <div class="flex items-center justify-between">
            <Button variant="ghost" size="sm" onclick={viewBuild}>View Build →</Button>
            <div class="flex gap-2">
              {#if hasShortfall}
                <Button size="sm" onclick={handleSubmitForFulfilment}>
                  <Send class="h-4 w-4 mr-1" /> Submit for Fulfilment
                </Button>
              {:else}
                <Button size="sm" onclick={handleCheckoutNow}>
                  <CheckCircle class="h-4 w-4 mr-1" /> Checkout Now
                </Button>
              {/if}
            </div>
          </div>

        {:else if buildAction === 'submitting' || buildAction === 'checking_out'}
          <div class="flex items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 class="h-4 w-4 animate-spin" />
            {buildAction === 'submitting' ? 'Submitting for fulfilment…' : 'Checking out…'}
          </div>
        {/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

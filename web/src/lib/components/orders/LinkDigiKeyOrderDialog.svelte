<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import { importOrder, type ImportOrderProgress } from '$lib/api/orders.js';
  import { ApiError } from '$lib/api/client.js';
  import { Loader2, XCircle, CheckCircle2, AlertTriangle } from 'lucide-svelte';

  let {
    teamId,
    orderId,
    open = $bindable(false),
    onSuccess,
  }: {
    teamId: string;
    orderId: string;
    open?: boolean;
    onSuccess?: () => void;
  } = $props();

  type Step = 'form' | 'mismatch' | 'progress' | 'result';

  let step = $state<Step>('form');
  let salesOrderId = $state('');
  let submitting = $state(false);
  let submitError = $state('');

  // Mismatch state
  let mismatchSkus = $state<string[]>([]);
  let mismatchQtyPct = $state(0);

  // Progress state
  let jobId = $state('');
  let progressTotal = $state(0);
  let progressProcessed = $state(0);
  let progressUnresolved = $state<string[]>([]);
  let progressError = $state('');

  // Result state
  let resultUnresolved = $state<string[]>([]);
  let resultExtraAdded = $state(0);

  function reset() {
    step = 'form';
    salesOrderId = '';
    submitting = false;
    submitError = '';
    mismatchSkus = [];
    mismatchQtyPct = 0;
    jobId = '';
    progressTotal = 0;
    progressProcessed = 0;
    progressUnresolved = [];
    progressError = '';
    resultUnresolved = [];
    resultExtraAdded = 0;
  }

  function handleOpenChange(v: boolean) {
    open = v;
    if (!v) reset();
  }

  async function submit(force = false) {
    submitting = true;
    submitError = '';
    try {
      const result = await importOrder(teamId, {
        sales_order_id: salesOrderId.trim(),
        existing_order_id: orderId,
        force,
      });
      jobId = result.job_id;
      step = 'progress';
    } catch (err: any) {
      if (err instanceof ApiError && err.status === 422 && err.code === 'order_mismatch') {
        mismatchSkus = (err.data.missing_skus as string[]) ?? [];
        mismatchQtyPct = (err.data.missing_qty_pct as number) ?? 0;
        step = 'mismatch';
      } else {
        submitError = err.message ?? 'Failed to start import.';
      }
    } finally {
      submitting = false;
    }
  }

  $effect(() => {
    if (step !== 'progress' || !jobId) return;
    const unsub = subscribeSSE(teamId, jobId, (event) => {
      const d = event.data as unknown as ImportOrderProgress;
      if (d.total !== undefined) progressTotal = d.total;
      if (d.processed !== undefined) progressProcessed = d.processed;
      if (d.unresolved_skus) progressUnresolved = d.unresolved_skus;

      if (d.status === 'complete') {
        resultUnresolved = d.unresolved_skus ?? [];
        resultExtraAdded = d.extra_items_added ?? 0;
        step = 'result';
        onSuccess?.();
      } else if (d.status === 'error') {
        progressError = d.error ?? 'Import failed.';
      }
    });
    return unsub;
  });

  let progressPct = $derived(progressTotal > 0 ? (progressProcessed / progressTotal) * 100 : 0);
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg"
    >
      {#if step === 'form'}
        <Dialog.Title class="text-lg font-semibold mb-4">Link DigiKey Order</Dialog.Title>
        <p class="text-sm text-muted-foreground mb-4">
          Enter the DigiKey sales order ID to link it to this partrack order. Prices and quantities will be updated from DigiKey's confirmed data.
        </p>
        <div class="space-y-1.5">
          <Label for="link-sales-order-id">DigiKey Sales Order ID</Label>
          <Input
            id="link-sales-order-id"
            placeholder="e.g. 12345678"
            value={salesOrderId}
            oninput={(e: Event) => { salesOrderId = (e.target as HTMLInputElement).value; }}
            onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') submit(); }}
            disabled={submitting}
          />
        </div>
        {#if submitError}
          <p class="text-sm text-destructive flex items-center gap-1.5 mt-3">
            <XCircle class="h-4 w-4 shrink-0" />{submitError}
          </p>
        {/if}
        <div class="flex justify-end gap-2 mt-6">
          <Button variant="outline" onclick={() => { open = false; }}>Cancel</Button>
          <Button onclick={() => submit()} disabled={submitting || !salesOrderId.trim()}>
            {#if submitting}<Loader2 class="h-4 w-4 animate-spin mr-1" />{/if}
            Link Order
          </Button>
        </div>

      {:else if step === 'mismatch'}
        <Dialog.Title class="text-lg font-semibold mb-3">Order Mismatch Warning</Dialog.Title>
        <div class="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/30 p-3 mb-4">
          <p class="text-sm font-medium flex items-center gap-1.5 text-amber-700 dark:text-amber-400 mb-2">
            <AlertTriangle class="h-4 w-4 shrink-0" />
            This DigiKey order may not match your partrack order
          </p>
          <p class="text-sm text-amber-700 dark:text-amber-400">
            {mismatchQtyPct.toFixed(0)}% of quantity is missing from the DigiKey order.
          </p>
          {#if mismatchSkus.length > 0}
            <ul class="mt-2 text-xs font-mono text-amber-700 dark:text-amber-400 space-y-0.5">
              {#each mismatchSkus.slice(0, 5) as sku}
                <li>{sku}</li>
              {/each}
              {#if mismatchSkus.length > 5}
                <li class="font-sans">… and {mismatchSkus.length - 5} more</li>
              {/if}
            </ul>
          {/if}
        </div>
        <div class="flex justify-end gap-2">
          <Button variant="outline" onclick={() => { step = 'form'; }}>Back</Button>
          <Button onclick={() => submit(true)} disabled={submitting}>
            {#if submitting}<Loader2 class="h-4 w-4 animate-spin mr-1" />{/if}
            Link Anyway
          </Button>
        </div>

      {:else if step === 'progress'}
        <Dialog.Title class="text-lg font-semibold mb-1">Linking Order</Dialog.Title>
        {#if progressError}
          <div class="flex items-center gap-2 py-4 text-destructive">
            <XCircle class="h-5 w-5 shrink-0" />
            <p class="text-sm">Failed: {progressError}</p>
          </div>
          <div class="flex justify-end mt-4">
            <Button variant="outline" onclick={() => { open = false; }}>Close</Button>
          </div>
        {:else}
          <div class="flex items-center gap-3 py-4">
            <Loader2 class="h-5 w-5 shrink-0 animate-spin text-muted-foreground" />
            <div class="flex-1 space-y-1.5">
              <p class="text-sm text-muted-foreground">
                Reconciling line items…{#if progressTotal > 0}&nbsp;{progressProcessed}/{progressTotal}{/if}
              </p>
              {#if progressTotal > 0}
                <div class="h-1.5 w-full overflow-hidden rounded-full bg-border">
                  <div class="h-full bg-green-500 transition-all" style="width: {progressPct}%"></div>
                </div>
              {/if}
            </div>
          </div>
        {/if}

      {:else if step === 'result'}
        <Dialog.Title class="text-lg font-semibold mb-1">Order Linked</Dialog.Title>
        <div class="flex items-center gap-2 py-2 text-green-600">
          <CheckCircle2 class="h-5 w-5 shrink-0" />
          <p class="text-sm">Order linked and set to placed status.</p>
        </div>
        {#if resultExtraAdded > 0}
          <p class="text-sm text-muted-foreground mt-2">
            {resultExtraAdded} item{resultExtraAdded !== 1 ? 's' : ''} from DigiKey were added to this order.
          </p>
        {/if}
        {#if resultUnresolved.length > 0}
          <div class="mt-3 rounded-md border border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/30 p-3">
            <p class="text-sm font-medium flex items-center gap-1.5 text-amber-700 dark:text-amber-400 mb-1.5">
              <AlertTriangle class="h-4 w-4 shrink-0" />
              {resultUnresolved.length} SKU{resultUnresolved.length !== 1 ? 's' : ''} could not be resolved
            </p>
            <ul class="text-xs text-amber-700 dark:text-amber-400 font-mono space-y-0.5 max-h-28 overflow-y-auto">
              {#each resultUnresolved.slice(0, 10) as sku}
                <li>{sku}</li>
              {/each}
              {#if resultUnresolved.length > 10}
                <li class="font-sans">… and {resultUnresolved.length - 10} more</li>
              {/if}
            </ul>
          </div>
        {/if}
        <div class="flex justify-end mt-5">
          <Button onclick={() => { open = false; }}>Done</Button>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

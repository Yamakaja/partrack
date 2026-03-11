<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import { importOrder, type ImportOrderProgress } from '$lib/api/orders.js';
  import { ApiError } from '$lib/api/client.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { Loader2, XCircle, CheckCircle2 } from 'lucide-svelte';
  import OrderProgressStep from './OrderProgressStep.svelte';
  import UnresolvedSkuList from './UnresolvedSkuList.svelte';

  let {
    teamId,
    open = $bindable(false),
    onSuccess,
  }: {
    teamId: string;
    open?: boolean;
    onSuccess?: () => void;
  } = $props();

  type Step = 'form' | 'progress' | 'result';

  let step = $state<Step>('form');
  let salesOrderId = $state('');
  let submitting = $state(false);
  let submitError = $state('');

  // Progress state
  let jobId = $state('');
  let progressTotal = $state(0);
  let progressResolved = $state(0);
  let progressUnresolved = $state<string[]>([]);
  let progressDone = $state(false);
  let progressError = $state('');

  // Result state
  let resultOrderId = $state('');
  let resultUnresolved = $state<string[]>([]);

  function reset() {
    step = 'form';
    salesOrderId = '';
    submitting = false;
    submitError = '';
    jobId = '';
    progressTotal = 0;
    progressResolved = 0;
    progressUnresolved = [];
    progressDone = false;
    progressError = '';
    resultOrderId = '';
    resultUnresolved = [];
  }

  function handleOpenChange(v: boolean) {
    open = v;
    if (!v) reset();
  }

  async function handleSubmit() {
    if (!salesOrderId.trim()) return;
    submitting = true;
    submitError = '';
    try {
      const result = await importOrder(teamId, { sales_order_id: salesOrderId.trim() });
      jobId = result.job_id;
      step = 'progress';
    } catch (err: any) {
      if (err instanceof ApiError && err.status === 409) {
        submitError = 'This DigiKey order has already been imported.';
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
      if (d.resolved !== undefined) progressResolved = d.resolved;
      if (d.unresolved_skus) progressUnresolved = d.unresolved_skus;

      if (d.status === 'complete') {
        resultOrderId = d.order_id ?? '';
        resultUnresolved = d.unresolved_skus ?? [];
        progressDone = true;
        step = 'result';
        onSuccess?.();
      } else if (d.status === 'error') {
        progressError = d.error ?? 'Import failed.';
      }
    });
    return unsub;
  });

</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg"
    >
      {#if step === 'form'}
        <Dialog.Title class="text-lg font-semibold mb-4">Import DigiKey Order</Dialog.Title>
        <div class="space-y-4">
          <div class="space-y-1.5">
            <Label for="sales-order-id">DigiKey Sales Order ID</Label>
            <Input
              id="sales-order-id"
              placeholder="e.g. 12345678"
              value={salesOrderId}
              oninput={(e: Event) => { salesOrderId = (e.target as HTMLInputElement).value; }}
              onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') handleSubmit(); }}
              disabled={submitting}
            />
          </div>
          {#if submitError}
            <p class="text-sm text-destructive flex items-center gap-1.5">
              <XCircle class="h-4 w-4 shrink-0" />{submitError}
            </p>
          {/if}
        </div>
        <div class="flex justify-end gap-2 mt-6">
          <Button variant="outline" onclick={() => { open = false; }}>Cancel</Button>
          <Button onclick={handleSubmit} disabled={submitting || !salesOrderId.trim()}>
            {#if submitting}<Loader2 class="h-4 w-4 animate-spin mr-1" />{/if}
            Import
          </Button>
        </div>

      {:else if step === 'progress'}
        <Dialog.Title class="text-lg font-semibold mb-1">Importing Order</Dialog.Title>
        <OrderProgressStep
          description="Resolving parts…"
          total={progressTotal}
          processed={progressResolved}
          processedLabel=" resolved"
          unresolved={progressUnresolved}
          error={progressError ? 'Import failed: ' + progressError : ''}
          onClose={() => { open = false; }}
        />

      {:else if step === 'result'}
        <Dialog.Title class="text-lg font-semibold mb-1">Order Imported</Dialog.Title>
        <div class="flex items-center gap-2 py-2 text-green-600">
          <CheckCircle2 class="h-5 w-5 shrink-0" />
          <p class="text-sm">Order successfully imported and set to placed status.</p>
        </div>
        <UnresolvedSkuList skus={resultUnresolved} />
        <div class="flex justify-end gap-2 mt-5">
          <Button variant="outline" onclick={() => { open = false; }}>Close</Button>
          {#if resultOrderId}
            <Button onclick={() => { open = false; navigate(`/teams/${teamId}/orders/${resultOrderId}`); }}>
              Go to Order
            </Button>
          {/if}
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

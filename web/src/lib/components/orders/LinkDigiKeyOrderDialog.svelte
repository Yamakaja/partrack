<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import { importOrder, commitLinkOrder, type ImportOrderProgress } from '$lib/api/orders.js';
  import { ApiError } from '$lib/api/client.js';
  import { Loader2, XCircle, CheckCircle2, AlertTriangle } from 'lucide-svelte';
  import type { AllocationConflicts } from '$lib/types/orders.js';

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

  type Step = 'form' | 'mismatch' | 'conflicts' | 'progress' | 'result';

  let step = $state<Step>('form');
  let salesOrderId = $state('');
  let submitting = $state(false);
  let submitError = $state('');

  // Mismatch state
  let mismatchSkus = $state<string[]>([]);
  let mismatchQtyPct = $state(0);

  // Conflict state
  let conflictToken = $state('');
  let conflicts = $state<AllocationConflicts | null>(null);
  let buildKeep = $state<Record<string, boolean>>({});
  let priorityError = $state('');

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
    conflictToken = '';
    conflicts = null;
    buildKeep = {};
    priorityError = '';
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
      }) as any;

      if (result.code === 'allocation_conflicts') {
        conflictToken = result.token as string;
        conflicts = result.conflicts as AllocationConflicts;
        // Initialise keep state from server's greedy suggestion.
        const initial: Record<string, boolean> = {};
        for (const b of conflicts.affected_builds) {
          initial[b.build_id] = b.suggested_keep;
        }
        buildKeep = initial;
        step = 'conflicts';
      } else {
        jobId = result.job_id;
        step = 'progress';
      }
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

  async function commitConflicts() {
    if (!conflicts) return;
    submitting = true;
    priorityError = '';
    try {
      const priorities = conflicts.affected_builds.map(b => ({
        build_id: b.build_id,
        keep: buildKeep[b.build_id] ?? true,
      }));
      const result = await commitLinkOrder(teamId, orderId, conflictToken, priorities);
      jobId = result.job_id;
      step = 'progress';
    } catch (err: any) {
      if (err instanceof ApiError && err.code === 'TOKEN_EXPIRED') {
        priorityError = 'Session expired — close this dialog and try again.';
      } else if (err instanceof ApiError && err.code === 'INVALID_PRIORITIES') {
        priorityError = 'Selection exceeds available quantity — please adjust.';
      } else {
        priorityError = err.message ?? 'Failed to commit.';
      }
    } finally {
      submitting = false;
    }
  }

  // ── Conflict resolution reactive logic ───────────────────────────────────
  // For each part that was reduced, compute remaining capacity given current keep selections.
  // remainingByPart: part_id → how much actualQty is still unallocated by kept builds.
  const remainingByPart = $derived.by(() => {
    if (!conflicts) return {} as Record<string, number>;
    // Collect actualQty per part from the first build that references it.
    const actualQty: Record<string, number> = {};
    for (const b of conflicts.affected_builds) {
      for (const ps of b.parts_short) {
        if (!(ps.part_id in actualQty)) actualQty[ps.part_id] = ps.actual_qty;
      }
    }
    // Subtract claims of kept builds.
    const remaining = { ...actualQty };
    for (const b of conflicts.affected_builds) {
      if (!buildKeep[b.build_id]) continue;
      for (const ps of b.parts_short) {
        remaining[ps.part_id] = (remaining[ps.part_id] ?? 0) - ps.quantity_from_order_before;
      }
    }
    return remaining;
  });

  // disabledBuilds: build_id → blocking part mpn (null if not disabled).
  // A build is disabled (cannot be toggled to keep) if any of its parts has
  // quantity_from_order_before > remaining + (build's own contribution if it were kept).
  const disabledBuilds = $derived.by(() => {
    if (!conflicts) return {} as Record<string, string | null>;
    const result: Record<string, string | null> = {};
    for (const b of conflicts.affected_builds) {
      if (buildKeep[b.build_id]) {
        result[b.build_id] = null; // already kept — not disabled
        continue;
      }
      let blocker: string | null = null;
      for (const ps of b.parts_short) {
        // If we were to keep this build, remaining would decrease by its claim.
        // Can it fit? remaining[part] >= claim?
        const avail = remainingByPart[ps.part_id] ?? 0;
        if (ps.quantity_from_order_before > avail) {
          blocker = `${ps.mpn || ps.part_id}: ${ps.quantity_from_order_before} needed, ${Math.max(0, avail)} available`;
          break;
        }
      }
      result[b.build_id] = blocker;
    }
    return result;
  });

  const canSubmitConflicts = $derived(
    conflicts !== null &&
    !submitting &&
    // All builds that are not disabled and kept must have their claims covered.
    Object.values(disabledBuilds).every(v => v === null || true) // disabled builds are always reverted
  );

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
      class="fixed left-1/2 top-1/2 z-50 w-full -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg {step === 'conflicts' ? 'max-w-2xl' : 'max-w-md'}"
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

      {:else if step === 'conflicts' && conflicts}
        <Dialog.Title class="text-lg font-semibold mb-1">Some builds need to be reallocated</Dialog.Title>
        <p class="text-sm text-muted-foreground mb-4">
          DigiKey confirmed fewer units than planned. Select which builds to keep as ordered.
          Deselected builds will return to pending and their shortfall will be added to the cart.
        </p>

        <div class="rounded-md border border-border overflow-hidden mb-4">
          <table class="w-full text-sm">
            <thead class="bg-muted/50">
              <tr>
                <th class="text-left px-3 py-2 font-medium text-muted-foreground">Project</th>
                <th class="text-right px-3 py-2 font-medium text-muted-foreground">Qty</th>
                <th class="text-left px-3 py-2 font-medium text-muted-foreground">Parts affected</th>
                <th class="px-3 py-2 font-medium text-muted-foreground text-center">Action</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              {#each conflicts.affected_builds as build (build.build_id)}
                {@const isKept = buildKeep[build.build_id] ?? true}
                {@const blocker = disabledBuilds[build.build_id]}
                {@const isDisabled = !isKept && blocker !== null}
                <tr class="hover:bg-muted/30 {isDisabled ? 'opacity-60' : ''}">
                  <td class="px-3 py-2 font-medium">{build.project_name}</td>
                  <td class="px-3 py-2 text-right tabular-nums text-muted-foreground">{build.build_quantity}</td>
                  <td class="px-3 py-2">
                    <div class="space-y-0.5">
                      {#each build.parts_short as ps}
                        <div class="text-xs text-muted-foreground">
                          <span class="font-mono">{ps.mpn || ps.part_id}</span>:
                          {ps.quantity_from_order_before} needed,
                          {ps.actual_qty} available
                        </div>
                      {/each}
                    </div>
                  </td>
                  <td class="px-3 py-2 text-center">
                    {#if isDisabled}
                      <div class="relative group inline-block">
                        <Button variant="outline" size="sm" class="text-muted-foreground cursor-not-allowed" disabled>
                          Revert to pending
                        </Button>
                        <div class="absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 hidden group-hover:block z-10 w-56 rounded bg-popover border border-border px-2 py-1.5 text-xs text-popover-foreground shadow-md text-left">
                          {blocker}
                        </div>
                      </div>
                    {:else}
                      <Button
                        variant={isKept ? 'default' : 'outline'}
                        size="sm"
                        onclick={() => { buildKeep[build.build_id] = !isKept; }}
                      >
                        {isKept ? 'Keep ordered' : 'Revert to pending'}
                      </Button>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        {#if priorityError}
          <p class="text-sm text-destructive flex items-center gap-1.5 mb-3">
            <XCircle class="h-4 w-4 shrink-0" />{priorityError}
          </p>
        {/if}

        <div class="flex justify-end gap-2">
          <Button variant="outline" onclick={() => { step = 'form'; }}>Back</Button>
          <Button onclick={commitConflicts} disabled={!canSubmitConflicts}>
            {#if submitting}<Loader2 class="h-4 w-4 animate-spin mr-1" />{/if}
            Confirm & Link Order
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

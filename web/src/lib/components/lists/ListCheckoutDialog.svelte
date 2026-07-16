<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { createCheckout, type InsufficientCheckoutItem } from '$lib/api/checkouts.js';
  import { ApiError } from '$lib/api/client.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import type { CollectionItem } from '$lib/api/collections.js';

  // Compact confirm for the list's one ledger-writing verb: a count summary
  // plus the reason General Checkout takes anyway — the list behind the dialog
  // is the item table. After success it offers to archive the (now physically
  // gone) pile's list.
  let {
    teamId,
    listName,
    items,
    open = $bindable(false),
    oncomplete,
  }: {
    teamId: string;
    listName: string;
    items: CollectionItem[];
    open?: boolean;
    oncomplete: (opts: { archive: boolean }) => void;
  } = $props();

  let reason = $state('');
  let submitting = $state(false);
  let step = $state<'confirm' | 'done'>('confirm');
  let insufficient = $state<InsufficientCheckoutItem[]>([]);

  const quantified = $derived(items.filter((i) => i.quantity != null));
  const skipped = $derived(items.length - quantified.length);
  const totalPieces = $derived(quantified.reduce((s, i) => s + (i.quantity ?? 0), 0));

  $effect(() => {
    if (open) {
      step = 'confirm';
      insufficient = [];
      reason = '';
    }
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (quantified.length === 0 || submitting) return;
    submitting = true;
    insufficient = [];
    try {
      await createCheckout(teamId, {
        reason: reason.trim() || undefined,
        items: quantified.map((i) => ({ part_id: i.part_id, quantity: i.quantity! })),
      });
      step = 'done';
    } catch (err) {
      if (err instanceof ApiError && err.code === 'INSUFFICIENT_STOCK') {
        insufficient = (err.data.insufficient_items ?? []) as InsufficientCheckoutItem[];
      } else {
        insufficient = [];
        pushToast('error', err instanceof Error ? err.message : 'Checkout failed');
      }
    } finally {
      submitting = false;
    }
  }

  function finish(archive: boolean) {
    open = false;
    oncomplete({ archive });
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      {#if step === 'confirm'}
        <Dialog.Title class="text-lg font-semibold mb-2">Check out “{listName}”</Dialog.Title>
        <p class="text-sm">
          Checking out <span class="font-semibold tabular-nums">{quantified.length}</span>
          part{quantified.length === 1 ? '' : 's'}
          (<span class="font-semibold tabular-nums">{totalPieces.toLocaleString()}</span> pieces total).
        </p>
        {#if skipped > 0}
          <p class="mt-1 text-xs text-warning">
            {skipped} row{skipped === 1 ? ' has' : 's have'} no quantity and will be skipped.
          </p>
        {/if}

        {#if insufficient.length > 0}
          <div class="mt-3 rounded-md border border-destructive/40 bg-destructive/5 p-2 text-xs">
            <p class="font-medium text-destructive">Not enough stock:</p>
            <ul class="mt-1 space-y-0.5">
              {#each insufficient as it (it.part_id)}
                <li class="flex justify-between gap-2">
                  <span class="truncate font-mono">{it.part_mpn ?? it.part_id}</span>
                  <span class="shrink-0 tabular-nums text-muted-foreground">
                    have {it.quantity_on_hand} / need {it.quantity_requested}
                  </span>
                </li>
              {/each}
            </ul>
            <p class="mt-1.5 text-muted-foreground">
              Fix quantities on the list, or reconcile stock via the Checkouts page.
            </p>
          </div>
        {/if}

        <form onsubmit={submit} class="mt-4 space-y-3">
          <div class="space-y-1">
            <Label for="list-checkout-reason">Reason</Label>
            <input
              id="list-checkout-reason"
              bind:value={reason}
              placeholder="e.g. bench cleanup"
              class="flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>
          <div class="flex justify-end gap-2 pt-1">
            <Dialog.Close>
              <Button variant="outline" type="button">Cancel</Button>
            </Dialog.Close>
            <Button type="submit" disabled={submitting || quantified.length === 0}>
              {submitting ? 'Checking out…' : `Check out ${quantified.length} part${quantified.length === 1 ? '' : 's'}`}
            </Button>
          </div>
        </form>
      {:else}
        <Dialog.Title class="text-lg font-semibold mb-2">Checked out</Dialog.Title>
        <p class="text-sm">
          {quantified.length} part{quantified.length === 1 ? '' : 's'} checked out from “{listName}”.
        </p>
        <p class="mt-1 text-sm text-muted-foreground">
          Archive this list? The pile is checked out — the list can sink.
        </p>
        <div class="mt-4 flex justify-end gap-2">
          <Button variant="outline" onclick={() => finish(false)}>Keep list</Button>
          <Button onclick={() => finish(true)}>Archive list</Button>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

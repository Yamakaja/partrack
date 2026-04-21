<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { createAdjustment } from '$lib/api/inventory.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import AdjustStockForm from './AdjustStockForm.svelte';

  let {
    open = $bindable(false),
    teamId,
    entryId,
    currentQuantity,
    mpn,
    manufacturer,
    onSuccess,
  }: {
    open: boolean;
    teamId: string;
    entryId: string;
    currentQuantity?: number;
    mpn?: string;
    manufacturer?: string;
    onSuccess?: () => void;
  } = $props();

  const qc = useQueryClient();

  let mode = $state<'delta' | 'set'>('delta');
  let value = $state('');
  let reason = $state('');
  let submitting = $state(false);

  function reset() {
    mode = 'delta';
    value = '';
    reason = '';
    submitting = false;
  }

  const isValid = $derived(() => {
    const n = parseInt(value, 10);
    if (isNaN(n)) return false;
    if (mode === 'delta') return n !== 0;
    return n >= 0 && n !== currentQuantity;
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
    const n = parseInt(value, 10);
    if (isNaN(n)) return;
    const d = mode === 'delta' ? n : n - (currentQuantity ?? 0);
    if (d === 0) return;
    submitting = true;
    try {
      await createAdjustment(teamId, entryId, { delta: d, reason: reason.trim() });
      open = false;
      qc.invalidateQueries({ queryKey: ['adjustments', teamId, entryId] });
      qc.invalidateQueries({ queryKey: ['parts', teamId] });
      const label = mode === 'delta'
        ? `Stock adjusted (${d >= 0 ? '+' : ''}${d})`
        : `Stock set to ${n}`;
      pushToast('success', label);
      onSuccess?.();
    } catch (err: any) {
      pushToast('error', 'Failed to adjust stock', err.message);
    } finally {
      submitting = false;
    }
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) reset(); }}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      onOpenAutoFocus={(e) => { e.preventDefault(); (document.getElementById('adj-value') as HTMLInputElement)?.focus(); }}
      class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg"
    >
      <Dialog.Title class="text-lg font-semibold">Adjust Stock</Dialog.Title>
      {#if mpn || manufacturer}
        <p class="mt-0.5 mb-4 text-sm text-muted-foreground">
          {#if mpn}<span class="font-mono">{mpn}</span>{/if}{#if mpn && manufacturer} · {/if}{#if manufacturer}{manufacturer}{/if}
        </p>
      {:else}
        <div class="mb-4"></div>
      {/if}
      <form onsubmit={handleSubmit} class="space-y-3">
        <AdjustStockForm {currentQuantity} bind:mode bind:value bind:reason />

        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => (open = false)}>Cancel</Button>
          <Button type="submit" disabled={submitting || !isValid()}>
            {submitting ? 'Applying…' : mode === 'set' ? 'Set' : 'Apply'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

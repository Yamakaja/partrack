<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { Dialog } from 'bits-ui';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { createAdjustment } from '$lib/api/inventory.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

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

  type Mode = 'delta' | 'set';
  let mode = $state<Mode>('delta');
  let value = $state('');
  let reason = $state('');
  let submitting = $state(false);

  function reset() {
    mode = 'delta';
    value = '';
    reason = '';
    submitting = false;
  }

  function switchMode(m: Mode) {
    mode = m;
    value = '';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === '=' && currentQuantity !== undefined) {
      e.preventDefault();
      switchMode('set');
    } else if ((e.key === '+' || e.key === '-') && mode === 'set') {
      switchMode('delta');
    }
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

        <!-- Mode toggle -->
        <div class="flex rounded-md border border-border overflow-hidden text-xs font-medium">
          <button
            type="button"
            onclick={() => switchMode('delta')}
            class="flex-1 px-3 py-1.5 transition-colors
              {mode === 'delta' ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground'}"
          >± Delta</button>
          <button
            type="button"
            onclick={() => switchMode('set')}
            disabled={currentQuantity === undefined}
            class="flex-1 px-3 py-1.5 border-l border-border transition-colors disabled:opacity-40
              {mode === 'set' ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground'}"
          >= Set to</button>
        </div>

        <div class="space-y-1">
          <Label for="adj-value">
            {#if mode === 'delta'}
              Delta <span class="text-muted-foreground text-xs">(positive or negative; press = to switch to set)</span>
            {:else}
              New quantity <span class="text-muted-foreground text-xs">(current: {currentQuantity})</span>
            {/if}
          </Label>
          <Input
            id="adj-value"
            type="number"
            bind:value
            onkeydown={handleKeydown}
            placeholder={mode === 'delta' ? 'e.g. +10 or -5' : 'e.g. 42'}
            required
          />
        </div>

        <div class="space-y-1">
          <Label for="adj-reason">Reason</Label>
          <Input id="adj-reason" bind:value={reason} placeholder="Optional reason" />
        </div>

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

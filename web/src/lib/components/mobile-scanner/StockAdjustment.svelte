<script lang="ts">
  import { untrack } from 'svelte';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import type { ScanPart } from './types.js';
  import { stockAdjustment, ScanExpiredError } from '$lib/api/scanner.js';

  let {
    token,
    part,
    mode = 'adjust',
    initialValue,
    initialReason,
    onBack,
    onDone,
    onExpired,
  }: {
    token: string;
    part: ScanPart;
    mode?: 'set' | 'adjust';
    initialValue?: number;
    initialReason?: string;
    onBack: () => void;
    onDone: () => void;
    onExpired: () => void;
  } = $props();

  // ── State ─────────────────────────────────────────────────────────────────

  let adjValue = $state(untrack(() => String(initialValue ?? (mode === 'set' ? 0 : -1))));
  let adjReason = $state(untrack(() => initialReason ?? ''));
  let adjSubmitting = $state(false);

  // ── Submit ────────────────────────────────────────────────────────────────

  async function submitAdj() {
    if (!part.inventory_entry_id || adjSubmitting) return;
    const parsed = parseInt(adjValue, 10);
    if (isNaN(parsed)) return;
    const delta = mode === 'set'
      ? parsed - (part.quantity_on_hand ?? 0)
      : parsed;
    if (delta === 0) return;
    adjSubmitting = true;
    try {
      const data = await stockAdjustment(token, {
        inventory_entry_id: part.inventory_entry_id,
        delta,
        reason: adjReason || undefined,
      });
      pushToast('success', 'Adjustment saved', `New quantity: ${data.new_quantity_on_hand}`);
      onDone();
    } catch (e) {
      if (e instanceof ScanExpiredError) { onExpired(); return; }
      // keep state for retry
    } finally {
      adjSubmitting = false;
    }
  }

  function isSubmitDisabled(): boolean {
    const parsed = parseInt(adjValue, 10);
    if (isNaN(parsed) || adjSubmitting) return true;
    const delta = mode === 'set' ? parsed - (part.quantity_on_hand ?? 0) : parsed;
    return delta === 0;
  }
</script>

<div class="flex flex-1 flex-col">
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <p class="font-medium">Adjust Stock</p>
  </div>

  <div class="flex-1 overflow-y-auto p-4 space-y-4">
    <div class="rounded-xl bg-zinc-900 p-4">
      <p class="font-medium">{part.mpn ?? '—'}</p>
      <p class="text-xs text-zinc-400 mt-0.5">Current stock: {part.quantity_on_hand ?? 0}</p>
    </div>

    <div class="space-y-3">
      <div>
        {#if mode === 'set'}
          <p class="mb-2 text-xs font-medium text-zinc-400 uppercase tracking-wide">Quantity</p>
        {:else}
          <p class="mb-2 text-xs font-medium text-zinc-400 uppercase tracking-wide">Delta (negative = remove)</p>
        {/if}
        <div class="flex items-center gap-3">
          <button
            onclick={() => { const v = parseInt(adjValue, 10) || 0; adjValue = String(v - 1); }}
            class="flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-700 text-lg active:bg-zinc-800"
          >−</button>
          <input
            type="number"
            bind:value={adjValue}
            inputmode="numeric"
            class="flex-1 rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-lg font-mono text-white text-center focus:outline-none focus:ring-2 focus:ring-white/30"
          />
          <button
            onclick={() => { const v = parseInt(adjValue, 10) || 0; adjValue = String(v + 1); }}
            class="flex h-10 w-10 items-center justify-center rounded-lg border border-zinc-700 text-lg active:bg-zinc-800"
          >+</button>
        </div>
      </div>

      <div>
        <p class="mb-1 text-xs font-medium text-zinc-400 uppercase tracking-wide">Reason (optional)</p>
        <input
          type="text"
          bind:value={adjReason}
          placeholder="e.g. Counted, damaged, etc."
          class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:ring-2 focus:ring-white/30"
        />
      </div>
    </div>

    <button
      onclick={submitAdj}
      disabled={isSubmitDisabled()}
      class="w-full rounded-lg bg-white py-3 text-base font-semibold text-black active:bg-zinc-200 disabled:opacity-50"
    >
      {adjSubmitting ? 'Saving…' : 'Confirm'}
    </button>
  </div>
</div>

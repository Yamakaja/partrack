<script lang="ts">
  import { untrack } from 'svelte';
  import type { ScanPart } from './types.js';

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

  // ── Internal screen state ─────────────────────────────────────────────────

  type Screen = 'qty' | 'done';
  let screen = $state<Screen>('qty');

  // ── State ─────────────────────────────────────────────────────────────────

  let adjValue = $state(untrack(() => String(initialValue ?? (mode === 'set' ? 0 : -1))));
  let adjReason = $state(untrack(() => initialReason ?? ''));
  let adjSubmitting = $state(false);
  let adjNewQty = $state<number | null>(null);

  // ── API ───────────────────────────────────────────────────────────────────

  async function apiFetch(method: string, path: string, body?: unknown): Promise<Response> {
    return fetch(path, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  }

  async function apiJSON<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await apiFetch(method, path, body);
    if (res.status === 401 || res.status === 410) {
      onExpired();
      throw new Error('expired');
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

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
      const data = await apiJSON<{ ok: boolean; new_quantity_on_hand: number }>(
        'POST',
        '/api/v1/scan/stock-adjustment',
        {
          inventory_entry_id: part.inventory_entry_id,
          delta,
          reason: adjReason || undefined,
        },
      );
      adjNewQty = data.new_quantity_on_hand;
      screen = 'done';
    } catch {
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

<!-- ── STOCK ADJ QTY ─────────────────────────────────────────────────────── -->
{#if screen === 'qty'}
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

<!-- ── STOCK ADJ DONE ────────────────────────────────────────────────────── -->
{:else if screen === 'done'}
  <div class="flex flex-1 flex-col items-center justify-center gap-6 px-8 text-center">
    <div class="flex h-16 w-16 items-center justify-center rounded-full bg-green-500/20">
      <svg class="h-8 w-8 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
      </svg>
    </div>
    <div>
      <p class="text-lg font-semibold">Adjustment saved</p>
      {#if adjNewQty !== null}
        <p class="mt-2 text-sm text-zinc-400">New stock: <span class="text-white font-medium">{adjNewQty}</span></p>
      {/if}
    </div>
    <button
      onclick={onDone}
      class="rounded-lg bg-white px-8 py-3 text-base font-semibold text-black active:bg-zinc-200"
    >
      Back to Menu
    </button>
  </div>
{/if}

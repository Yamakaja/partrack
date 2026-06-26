<script lang="ts">
  import { untrack } from 'svelte';
  import type { ScanPart } from './types.js';
  import { searchParts, ScanExpiredError } from '$lib/api/scanner.js';

  let {
    initialQuery = '',
    onBack,
    onSelectPart,
    onExpired,
  }: {
    initialQuery?: string;
    onBack: () => void;
    onSelectPart: (part: ScanPart) => void;
    onExpired: () => void;
  } = $props();

  // ── State ─────────────────────────────────────────────────────────────────

  let partSearchQ = $state(untrack(() => initialQuery));
  let partResults = $state<ScanPart[]>([]);
  let partSearchLoading = $state(false);
  let searchError = $state(false);
  let partSearchDebounce: ReturnType<typeof setTimeout> | null = null;

  // ── Search ────────────────────────────────────────────────────────────────

  function doSearch(q: string) {
    if (partSearchDebounce) clearTimeout(partSearchDebounce);
    partSearchDebounce = setTimeout(async () => {
      if (!q.trim()) { partResults = []; searchError = false; return; }
      partSearchLoading = true;
      searchError = false;
      try {
        const data = await searchParts(q);
        partResults = data.parts;
      } catch (e) {
        if (e instanceof ScanExpiredError) { onExpired(); return; }
        partResults = [];
        searchError = true;
      } finally {
        partSearchLoading = false;
      }
    }, 300);
  }

  // Kick off search if we arrived with a pre-filled query.
  untrack(() => {
    if (initialQuery) doSearch(initialQuery);
  });
</script>

<!-- ── FIND PART SEARCH ──────────────────────────────────────────────────── -->
<div class="flex flex-1 flex-col">
  <div class="flex items-center gap-3 px-4 py-3 border-b border-zinc-800">
    <button onclick={onBack} aria-label="Back" class="text-zinc-400 active:text-white shrink-0">
      <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
      </svg>
    </button>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      type="search"
      placeholder="Search parts…"
      bind:value={partSearchQ}
      oninput={() => doSearch(partSearchQ)}
      autofocus
      class="flex-1 bg-transparent text-white placeholder-zinc-500 focus:outline-none text-sm"
    />
    {#if partSearchLoading}
      <div class="h-4 w-4 animate-spin rounded-full border border-zinc-600 border-t-white shrink-0"></div>
    {/if}
  </div>

  <div class="flex-1 overflow-y-auto divide-y divide-zinc-800">
    {#if searchError}
      <div class="py-12 text-center text-red-400 text-sm">Search failed — try again</div>
    {:else if partResults.length === 0 && partSearchQ.trim()}
      <div class="py-12 text-center text-zinc-400 text-sm">No parts found</div>
    {:else}
      {#each partResults as part}
        <button
          onclick={() => onSelectPart(part)}
          class="w-full px-4 py-3 text-left active:bg-zinc-900"
        >
          <p class="font-medium text-sm">{part.mpn ?? '—'}</p>
          {#if part.description}
            <p class="text-xs text-zinc-400 mt-0.5 line-clamp-1">{part.description}</p>
          {/if}
          {#if part.quantity_on_hand !== null}
            <p class="text-xs text-zinc-500 mt-0.5">Stock: {part.quantity_on_hand}</p>
          {/if}
        </button>
      {/each}
    {/if}
  </div>
</div>

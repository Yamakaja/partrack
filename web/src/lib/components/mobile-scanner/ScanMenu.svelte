<script lang="ts">
  import { scanHistory, type ScanHistoryEntry } from '$lib/stores/scannerHistory.svelte.js';

  let {
    contextOrderRef,
    onReceiveOrder,
    onBagScan,
    onFindPart,
    onPartSelect,
    onStorageAssignment,
  }: {
    contextOrderRef: string | null;
    onReceiveOrder: () => void;
    onBagScan: () => void;
    onFindPart: () => void;
    onPartSelect: (entry: ScanHistoryEntry) => void;
    onStorageAssignment: () => void;
  } = $props();

  function relativeTime(ts: number): string {
    const diff = Math.floor((Date.now() - ts) / 1000);
    if (diff < 60) return '< 1 min';
    if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)} hr ago`;
    return `${Math.floor(diff / 86400)}d ago`;
  }
</script>

<div class="flex flex-1 flex-col overflow-hidden">
  <!-- Header -->
  <div class="px-4 py-4 border-b border-zinc-800 shrink-0">
    <p class="text-lg font-semibold">Scanner</p>
    {#if contextOrderRef}
      <p class="text-xs text-zinc-400 mt-0.5">
        Order context: <span class="text-zinc-200">{contextOrderRef}</span>
      </p>
    {/if}
  </div>

  <!-- Menu items -->
  <div class="flex flex-col gap-2 p-4 shrink-0">
    <button
      onclick={onBagScan}
      class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
    >
      <div class="flex h-10 w-10 items-center justify-center rounded-full bg-purple-500/20 text-purple-400">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M3.75 4.875c0-.621.504-1.125 1.125-1.125h4.5c.621 0 1.125.504 1.125 1.125v4.5c0 .621-.504 1.125-1.125 1.125h-4.5A1.125 1.125 0 013.75 9.375v-4.5zM3.75 14.625c0-.621.504-1.125 1.125-1.125h4.5c.621 0 1.125.504 1.125 1.125v4.5c0 .621-.504 1.125-1.125 1.125h-4.5a1.125 1.125 0 01-1.125-1.125v-4.5zM13.5 4.875c0-.621.504-1.125 1.125-1.125h4.5c.621 0 1.125.504 1.125 1.125v4.5c0 .621-.504 1.125-1.125 1.125h-4.5A1.125 1.125 0 0113.5 9.375v-4.5z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.5 14.625v5.25M16.125 17.25h-5.25" />
        </svg>
      </div>
      <div>
        <p class="font-medium">Scan</p>
        <p class="text-xs text-zinc-400">Look up part &amp; manage inventory</p>
      </div>
    </button>
    
    <button
      onclick={onReceiveOrder}
      class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
    >
      <div class="flex h-10 w-10 items-center justify-center rounded-full bg-blue-500/20 text-blue-400">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M3 9a2 2 0 012-2h.93a2 2 0 001.664-.89l.812-1.22A2 2 0 0110.07 4h3.86a2 2 0 011.664.89l.812 1.22A2 2 0 0018.07 7H19a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2V9z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 13a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
      </div>
      <div>
        <p class="font-medium">Receive Order</p>
        <p class="text-xs text-zinc-400">
          {contextOrderRef ? `Receive ${contextOrderRef}` : 'Pick an order to receive'}
        </p>
      </div>
    </button>

    <button
      onclick={onFindPart}
      class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
    >
      <div class="flex h-10 w-10 items-center justify-center rounded-full bg-green-500/20 text-green-400">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
      </div>
      <div>
        <p class="font-medium">Find Part</p>
        <p class="text-xs text-zinc-400">Search catalog and check stock</p>
      </div>
    </button>

    <!-- Storage -->
    <button
      onclick={onStorageAssignment}
      class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
    >
      <div class="flex h-10 w-10 items-center justify-center rounded-full bg-amber-500/20 text-amber-400">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10" />
        </svg>
      </div>
      <div>
        <p class="font-medium">Storage</p>
        <p class="text-xs text-zinc-400">Assign parts to storage locations</p>
      </div>
    </button>
  </div>

  <!-- Recent parts -->
  {#if scanHistory.length > 0}
    <div class="flex flex-col flex-1 overflow-hidden border-t border-zinc-800">
      <p class="px-4 pt-3 pb-2 text-xs font-medium text-zinc-500 uppercase tracking-wide shrink-0">Recent</p>
      <div class="flex-1 overflow-y-auto px-4 pb-4 space-y-2">
        {#each scanHistory as entry (entry.partId)}
          <button
            onclick={() => onPartSelect(entry)}
            class="w-full flex items-center gap-3 rounded-xl bg-zinc-900 px-3 py-2.5 text-left active:bg-zinc-800"
          >
            <!-- Thumbnail -->
            <div class="h-10 w-10 shrink-0 rounded-lg overflow-hidden bg-zinc-800 flex items-center justify-center">
              {#if entry.imageUrl}
                <img src={entry.imageUrl} alt={entry.mpn ?? ''} class="h-full w-full object-contain p-1" />
              {:else}
                <svg class="h-5 w-5 text-zinc-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                    d="M9 3H5a2 2 0 00-2 2v4m6-6h10a2 2 0 012 2v4M9 3v18m0 0h10a2 2 0 002-2V9M9 21H5a2 2 0 01-2-2V9m0 0h18" />
                </svg>
              {/if}
            </div>
            <!-- Info -->
            <div class="flex-1 min-w-0">
              <div class="flex items-baseline justify-between gap-2">
                <p class="font-mono text-sm font-medium truncate">{entry.mpn ?? '—'}</p>
                <span class="shrink-0 text-xs text-zinc-600">{relativeTime(entry.timestamp)}</span>
              </div>
              {#if entry.description}
                <p class="text-xs text-zinc-400 truncate">{entry.description}</p>
              {/if}
            </div>
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>

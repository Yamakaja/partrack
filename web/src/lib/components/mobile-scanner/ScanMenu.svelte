<script lang="ts">
  let {
    contextOrderRef,
    onReceiveOrder,
    onBagScan,
    onFindPart,
    onStockAdj,
  }: {
    contextOrderRef: string | null;
    onReceiveOrder: () => void;
    onBagScan: () => void;
    onFindPart: () => void;
    onStockAdj: () => void;
  } = $props();
</script>

<div class="flex flex-1 flex-col">
  <!-- Header -->
  <div class="px-4 py-4 border-b border-zinc-800">
    <p class="text-lg font-semibold">Scanner</p>
    {#if contextOrderRef}
      <p class="text-xs text-zinc-400 mt-0.5">
        Order context: <span class="text-zinc-200">{contextOrderRef}</span>
      </p>
    {/if}
  </div>

  <!-- Menu items -->
  <div class="flex flex-col gap-2 p-4">
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
        <p class="font-medium">Scan Bag</p>
        <p class="text-xs text-zinc-400">Look up part &amp; manage inventory</p>
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

    <button
      onclick={onStockAdj}
      class="flex items-center gap-3 rounded-xl bg-zinc-900 px-4 py-4 text-left active:bg-zinc-800"
    >
      <div class="flex h-10 w-10 items-center justify-center rounded-full bg-orange-500/20 text-orange-400">
        <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
            d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
        </svg>
      </div>
      <div>
        <p class="font-medium">Stock Adjustment</p>
        <p class="text-xs text-zinc-400">Adjust inventory quantity</p>
      </div>
    </button>
  </div>
</div>

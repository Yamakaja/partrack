<script lang="ts">
  import { createInfiniteQuery, useQueryClient, type InfiniteData } from '@tanstack/svelte-query';
  import { listScanAuditLog, type ScanAuditEntry, type ScanAuditLogPage } from '$lib/api/scanAuditLog.js';
  import AdjustStockDialog from '$lib/components/inventory/AdjustStockDialog.svelte';
  import { navigate } from '$lib/router/index.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';

  let { teamId }: { teamId: string } = $props();

  // ── Filter state ─────────────────────────────────────────────────────────

  type Filter = 'all' | 'parts' | 'orders' | 'stock' | 'storage' | 'kitting';
  type Cursor = { cursorTime: string; cursorId: string };

  const filterEventTypes: Record<Filter, string[]> = {
    all: [],
    parts: ['part_imported', 'part_not_found', 'bag_scanned'],
    orders: ['order_received'],
    stock: ['stock_adjusted'],
    storage: ['storage_assigned'],
    kitting: ['kitting_confirmed', 'kitting_shortfall'],
  };

  const filterLabels: Record<Filter, string> = {
    all: 'All',
    parts: 'Parts',
    orders: 'Orders',
    stock: 'Stock',
    storage: 'Storage',
    kitting: 'Kitting',
  };

  let activeFilter = $state<Filter>('all');

  // ── Collapsed state (localStorage) ───────────────────────────────────────

  function readCollapsed(): boolean {
    try {
      return localStorage.getItem('partrack:activity-feed:collapsed') === 'true';
    } catch {
      return false;
    }
  }

  let collapsed = $state(readCollapsed());

  function toggleCollapsed() {
    collapsed = !collapsed;
    try {
      localStorage.setItem('partrack:activity-feed:collapsed', String(collapsed));
    } catch {}
  }

  // ── Infinite query ────────────────────────────────────────────────────────

  const query = createInfiniteQuery<ScanAuditLogPage, Error, InfiniteData<ScanAuditLogPage>, unknown[], Cursor | null>(() => ({
    queryKey: ['scan-audit-log', teamId, activeFilter],
    queryFn: ({ pageParam }) => {
      return listScanAuditLog(teamId, {
        eventTypes: filterEventTypes[activeFilter],
        cursorTime: pageParam?.cursorTime,
        cursorId: pageParam?.cursorId,
        limit: 50,
      });
    },
    initialPageParam: null,
    getNextPageParam: (lastPage): Cursor | null => {
      if (!lastPage.next_cursor_time) return null;
      return { cursorTime: lastPage.next_cursor_time, cursorId: lastPage.next_cursor_id ?? '' };
    },
    staleTime: 30_000,
  }));

  const allItems = $derived((query.data?.pages ?? []).flatMap((p) => p.items));

  // ── Quick stock adjust state ──────────────────────────────────────────────

  const queryClient = useQueryClient();
  let adjustDialogOpen = $state(false);
  let adjustEntryId = $state<string | null>(null);
  let adjustCurrentQty = $state<number | undefined>(undefined);
  let adjustMpn = $state<string | undefined>(undefined);
  let adjustManufacturer = $state<string | undefined>(undefined);

  function openAdjust(entryId: string, currentQuantity: number | null, mpn: string | null, manufacturer: string | null) {
    adjustEntryId = entryId;
    adjustCurrentQty = currentQuantity ?? undefined;
    adjustMpn = mpn ?? undefined;
    adjustManufacturer = manufacturer ?? undefined;
    adjustDialogOpen = true;
  }

  function handleAdjustSuccess() {
    queryClient.invalidateQueries({ queryKey: ['scan-audit-log', teamId] });
  }

  // ── Infinite scroll sentinel ──────────────────────────────────────────────

  let sentinel = $state<HTMLDivElement | undefined>(undefined);

  $effect(() => {
    if (!sentinel) return;
    const obs = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && query.hasNextPage && !query.isFetchingNextPage) {
          query.fetchNextPage();
        }
      },
      { threshold: 0.1 },
    );
    obs.observe(sentinel);
    return () => obs.disconnect();
  });

  // ── Helpers ───────────────────────────────────────────────────────────────

  function entityUrl(item: ScanAuditEntry): string | null {
    const tid = activeTeamStore.current?.teamId;
    if (!tid || !item.entity_id) return null;
    switch (item.entity_type) {
      case 'part':    return `/teams/${tid}/parts/${item.entity_id}`;
      case 'order':   return `/teams/${tid}/orders/${item.entity_id}`;
      case 'storage': return `/teams/${tid}/storage/${item.entity_id}`;
      case 'build':   return `/teams/${tid}/builds/${item.entity_id}`;
      default: return null;
    }
  }

  function relativeTime(iso: string): string {
    const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return `${Math.floor(diff / 86400)}d ago`;
  }

  function eventLabel(eventType: string): string {
    switch (eventType) {
      case 'order_received':    return 'Received';
      case 'stock_adjusted':    return 'Stock adj.';
      case 'storage_assigned':  return 'Stored';
      case 'part_imported':     return 'Imported';
      case 'part_not_found':    return 'Not found';
      case 'bag_scanned':       return 'Bag scan';
      case 'kitting_confirmed': return 'Kitted';
      case 'kitting_shortfall': return 'Shortfall';
      default: return eventType;
    }
  }

  function eventIcon(eventType: string): string {
    switch (eventType) {
      case 'order_received':    return '📦';
      case 'stock_adjusted':    return '±';
      case 'storage_assigned':  return '📍';
      case 'part_imported':     return '✦';
      case 'part_not_found':    return '?';
      case 'bag_scanned':       return '⬡';
      case 'kitting_confirmed': return '✓';
      case 'kitting_shortfall': return '⚠';
      default: return '·';
    }
  }
</script>

<!-- Collapsed: small floating badge -->
{#if collapsed}
  <button
    onclick={toggleCollapsed}
    class="fixed right-0 top-1/2 z-50 -translate-y-1/2 flex flex-col items-center gap-1 rounded-l-lg bg-card border border-border border-r-0 px-2 py-3 text-muted-foreground hover:text-foreground transition-colors shadow-lg"
    aria-label="Open scan activity"
  >
    <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
        d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
    </svg>
    <span class="text-[10px] font-medium tracking-wide" style="writing-mode: vertical-lr">Activity</span>
  </button>

<!-- Expanded: full panel -->
{:else}
  <div class="fixed right-0 top-0 z-50 hidden lg:flex h-full w-[380px] flex-col border-l border-border bg-card shadow-2xl">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
      <span class="text-sm font-semibold text-foreground">Scan Activity</span>
      <button
        onclick={toggleCollapsed}
        class="text-muted-foreground hover:text-foreground transition-colors"
        aria-label="Collapse"
      >
        <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
      </button>
    </div>

    <!-- Filter chips -->
    <div class="flex gap-1 px-3 py-2 border-b border-border overflow-x-auto shrink-0">
      {#each Object.entries(filterLabels) as [key, label]}
        <button
          onclick={() => { activeFilter = key as Filter; }}
          class="shrink-0 rounded-full px-2.5 py-0.5 text-xs font-medium transition-colors
            {activeFilter === key
              ? 'bg-foreground text-background'
              : 'bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground'}"
        >
          {label}
        </button>
      {/each}
    </div>

    <!-- List -->
    <div class="flex-1 overflow-y-auto">
      {#if query.isLoading}
        <div class="flex items-center justify-center p-8 text-muted-foreground text-sm">Loading…</div>
      {:else if query.isError}
        <div class="p-4 text-xs text-destructive">Failed to load activity.</div>
      {:else if allItems.length === 0}
        <div class="flex flex-col items-center justify-center p-8 text-muted-foreground text-sm gap-2">
          <svg class="h-8 w-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
              d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
          </svg>
          <span>No scan activity yet</span>
        </div>
      {:else}
        {#each allItems as item (item.id)}
          {@const url = entityUrl(item)}
          {@const isNotFound = item.event_type === 'part_not_found'}
          <div
            class="group flex gap-3 px-4 py-3 border-b border-border transition-colors
              {isNotFound ? 'bg-amber-50 dark:bg-amber-950/30' : 'hover:bg-accent/50'}"
          >
            <!-- Icon -->
            <div class="shrink-0 w-7 h-7 rounded-full flex items-center justify-center text-xs
              {isNotFound
                ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-400'
                : 'bg-muted text-muted-foreground'}">
              {eventIcon(item.event_type)}
            </div>

            <!-- Content -->
            <div class="flex-1 min-w-0">
              <div class="flex items-center justify-between gap-2">
                <span class="text-xs font-medium
                  {isNotFound ? 'text-amber-700 dark:text-amber-400' : 'text-foreground'}">
                  {eventLabel(item.event_type)}
                </span>
                <span class="text-[10px] text-muted-foreground shrink-0">{relativeTime(item.created_at)}</span>
              </div>

              {#if item.entity_name}
                <p class="text-xs text-muted-foreground font-mono truncate mt-0.5">{item.entity_name}</p>
              {:else if item.parsed_mpn}
                <p class="text-xs text-muted-foreground font-mono truncate mt-0.5">{item.parsed_mpn}</p>
              {:else if item.parsed_sku}
                <p class="text-xs text-muted-foreground font-mono truncate mt-0.5">{item.parsed_sku}</p>
              {/if}

              {#if item.part_manufacturer || item.part_category_name}
                <p class="text-[10px] text-muted-foreground truncate mt-0.5">
                  {[item.part_manufacturer, item.part_category_name].filter(Boolean).join(' · ')}
                </p>
              {/if}

              {#if item.part_short_description}
                <p class="text-[10px] text-muted-foreground truncate">{item.part_short_description}</p>
              {/if}

              {#if item.quantity != null}
                <p class="text-[10px] text-muted-foreground mt-0.5">qty {item.quantity}</p>
              {/if}

              <!-- Quick actions row -->
              <div class="mt-1 flex items-center gap-3">
                {#if url}
                  <button
                    onclick={() => navigate(url)}
                    class="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                  >
                    View →
                  </button>
                {:else if isNotFound && (item.parsed_sku || item.parsed_mpn)}
                  <span class="text-[10px] text-amber-600 dark:text-amber-500">
                    {item.entity_id ? 'Resolved ✓' : 'Unresolved'}
                  </span>
                {/if}
                {#if item.inventory_entry_id}
                  <button
                    onclick={() => openAdjust(item.inventory_entry_id!, item.inventory_quantity_on_hand, item.entity_name, item.part_manufacturer)}
                    class="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                  >
                    ± Adjust
                  </button>
                {/if}
              </div>
            </div>
          </div>
        {/each}

        <!-- Infinite scroll sentinel -->
        <div bind:this={sentinel} class="h-4"></div>
        {#if query.isFetchingNextPage}
          <div class="py-2 text-center text-xs text-muted-foreground">Loading more…</div>
        {/if}
      {/if}
    </div>
  </div>
{/if}

{#if adjustEntryId}
  <AdjustStockDialog
    bind:open={adjustDialogOpen}
    {teamId}
    entryId={adjustEntryId}
    currentQuantity={adjustCurrentQty}
    mpn={adjustMpn}
    manufacturer={adjustManufacturer}
    onSuccess={handleAdjustSuccess}
  />
{/if}

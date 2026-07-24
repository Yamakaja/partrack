<script lang="ts">
  import { createInfiniteQuery, useQueryClient, type InfiniteData } from '@tanstack/svelte-query';
  import { listActivity, type ActivityLogPage } from '$lib/api/activityLog.js';
  import ActivityEntry from '$lib/components/activity/ActivityEntry.svelte';
  import AdjustStockDialog from '$lib/components/inventory/AdjustStockDialog.svelte';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';

  let { teamId }: { teamId: string } = $props();

  // ── Scope (My / Team) ──────────────────────────────────────────────────────

  // The team-wide feed is gated to purchaser+; everyone sees their own activity.
  const roleRank: Record<string, number> = { viewer: 0, member: 1, purchaser: 2, admin: 3 };
  const canTeamScope = $derived((roleRank[activeTeamStore.current?.role ?? 'viewer'] ?? 0) >= 2);
  let scope = $state<'mine' | 'team'>('mine');

  // ── Filter state ─────────────────────────────────────────────────────────

  type Filter = 'all' | 'parts' | 'stock' | 'orders' | 'builds' | 'projects';
  type Cursor = { cursorTime: string; cursorId: string };

  // Maps a filter chip to the activity actions it selects. Each chip spans both
  // feeds — the web action and its scanner equivalent are the same thing to a
  // reader, and the panel is no longer scanner-only.
  const filterActions: Record<Filter, string[]> = {
    all: [],
    parts: ['part.created', 'scan.part_imported', 'scan.part_not_found', 'scan.bag_scanned'],
    stock: ['inventory.adjusted', 'checkout.created', 'scan.stock_adjusted', 'scan.storage_assigned'],
    orders: ['order.item_received', 'order.cancelled', 'order.external_linked', 'cart.converted', 'scan.order_received'],
    builds: ['build.submitted', 'build.assembled', 'build.cancelled', 'scan.kitting_confirmed', 'scan.kitting_shortfall'],
    projects: ['project.created', 'bom.imported'],
  };

  const filterLabels: Record<Filter, string> = {
    all: 'All',
    parts: 'Parts',
    stock: 'Stock',
    orders: 'Orders',
    builds: 'Builds',
    projects: 'Projects',
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

  const query = createInfiniteQuery<ActivityLogPage, Error, InfiniteData<ActivityLogPage>, unknown[], Cursor | null>(() => ({
    queryKey: ['activity-log', teamId, scope, activeFilter],
    queryFn: ({ pageParam }) => {
      return listActivity(teamId, {
        scope,
        actions: filterActions[activeFilter],
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
    queryClient.invalidateQueries({ queryKey: ['activity-log', teamId] });
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
</script>

<!-- Collapsed: floating tab (no layout space) -->
{#if collapsed}
  <button
    onclick={toggleCollapsed}
    class="fixed right-0 top-1/2 z-50 -translate-y-1/2 hidden lg:flex flex-col items-center gap-1 rounded-l-lg bg-card border border-border border-r-0 px-2 py-3 text-muted-foreground hover:text-foreground transition-colors shadow-lg"
    aria-label="Open recent activity"
  >
    <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
        d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2" />
    </svg>
    <span class="text-[10px] font-medium tracking-wide" style="writing-mode: vertical-lr">Activity</span>
  </button>

<!-- Expanded: inline panel -->
{:else}
  <div class="hidden lg:flex h-full w-[380px] shrink-0 flex-col border-l border-border bg-card">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
      <span class="text-sm font-semibold text-foreground">Recent Activity</span>
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

    <!-- Scope toggle (team scope gated to purchaser+) -->
    {#if canTeamScope}
      <div class="flex gap-1 px-3 pt-2 shrink-0">
        {#each [['mine', 'Mine'], ['team', 'Team']] as [key, label]}
          <button
            onclick={() => { scope = key as 'mine' | 'team'; }}
            class="flex-1 rounded-md px-2 py-1 text-xs font-medium transition-colors
              {scope === key
                ? 'bg-foreground text-background'
                : 'bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground'}"
          >
            {label}
          </button>
        {/each}
      </div>
    {/if}

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
          <span>No recent activity yet</span>
        </div>
      {:else}
        {#each allItems as item (item.id)}
          <ActivityEntry {item} {teamId} showActor={scope === 'team'} onAdjust={openAdjust} />
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

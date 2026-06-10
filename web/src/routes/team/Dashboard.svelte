<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import {
    Package, Warehouse, FolderOpen, ShoppingCart, ClipboardList,
    Hammer, PackageMinus,
  } from 'lucide-svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import Skeleton from '$lib/components/ui/skeleton/Skeleton.svelte';
  import { linkAction, navigate } from '$lib/router/index.svelte.js';
  import { getDashboard } from '$lib/api/dashboard.js';
  import { projectStatusVariant } from '$lib/types/projects';
  import { setPartsStockStatus } from '$lib/stores/navState.js';
  import { authStore } from '$lib/stores/auth.svelte.js';
  import BuildPipelineCards from '$lib/components/builds/BuildPipelineCards.svelte';
  import ActivityEntry from '$lib/components/activity/ActivityEntry.svelte';
  import { formatRelative } from '$lib/utils/date.js';

  let { teamId }: { teamId: string } = $props();

  let dashQuery = createQuery(() => ({
    queryKey: ['dashboard', teamId],
    queryFn: () => getDashboard(teamId),
  }));

  const navCards = [
    { title: 'Parts', description: 'Browse and manage the parts catalog.', icon: Package, href: () => `/teams/${teamId}/parts` },
    { title: 'Storage', description: 'Manage storage locations.', icon: Warehouse, href: () => `/teams/${teamId}/storage` },
    { title: 'Projects', description: 'Manage PCB projects and BOMs.', icon: FolderOpen, href: () => `/teams/${teamId}/projects` },
    { title: 'Cart', description: 'Parts staged for ordering.', icon: ShoppingCart, href: () => `/teams/${teamId}/cart` },
    { title: 'Orders', description: 'Track orders with distributors.', icon: ClipboardList, href: () => `/teams/${teamId}/orders` },
    { title: 'Builds', description: 'Record PCB assembly events.', icon: Hammer, href: () => `/teams/${teamId}/builds` },
    { title: 'Checkouts', description: 'Ad-hoc parts withdrawals.', icon: PackageMinus, href: () => `/teams/${teamId}/checkouts` },
  ];
</script>

<div class="flex flex-col h-full">
  <Topbar title="Dashboard" />

  <div class="flex-1 overflow-auto p-6 space-y-6">

    <!-- Stats Row -->
    <section class="space-y-2">
      <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Overview</h3>
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        {#if dashQuery.isPending}
          {#each [0, 1, 2, 3] as _}
            <Skeleton class="h-20 rounded-lg" />
          {/each}
        {:else if dashQuery.data}
          {@const s = dashQuery.data.stats}

          <a href={`/teams/${teamId}/projects`} use:linkAction
            class="rounded-lg border border-border bg-card p-4 no-underline hover:bg-accent transition-colors flex flex-col gap-1">
            <p class="text-xs text-muted-foreground">Projects</p>
            <p class="text-2xl font-bold tabular-nums">{s.active_project_count}</p>
            <p class="text-xs text-muted-foreground">active · {s.project_count} total</p>
          </a>

          <a href={`/teams/${teamId}/inventory`} use:linkAction
            class="rounded-lg border border-border bg-card p-4 no-underline hover:bg-accent transition-colors flex flex-col gap-1">
            <p class="text-xs text-muted-foreground">Parts Tracked</p>
            <p class="text-2xl font-bold tabular-nums">{s.tracked_part_count}</p>
            <p class="text-xs text-muted-foreground">{s.in_stock_part_count} in stock</p>
          </a>

          <a href={`/teams/${teamId}/cart`} use:linkAction
            class="rounded-lg border border-border bg-card p-4 no-underline hover:bg-accent transition-colors flex flex-col gap-1">
            <p class="text-xs text-muted-foreground">Cart</p>
            <p class="text-2xl font-bold tabular-nums">{s.cart_item_count}</p>
            <p class="text-xs text-muted-foreground">items staged</p>
          </a>

          <button
            onclick={() => { setPartsStockStatus('low_stock'); navigate(`/teams/${teamId}/parts`); }}
            class="rounded-lg border border-border bg-card p-4 text-left hover:bg-accent transition-colors flex flex-col gap-1{s.inventory_below_restock_count > 0 ? ' border-yellow-500/50' : ''}">
            <p class="text-xs text-muted-foreground">Inventory Alerts</p>
            <p class="text-2xl font-bold tabular-nums{s.inventory_below_restock_count > 0 ? ' text-yellow-600 dark:text-yellow-400' : ''}">{s.inventory_below_restock_count}</p>
            <p class="text-xs text-muted-foreground">below restock level</p>
          </button>
        {/if}
      </div>
    </section>

    <!-- Build Pipeline -->
    <section class="space-y-2">
      <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Build Pipeline</h3>
      <BuildPipelineCards
        {teamId}
        pipeline={dashQuery.data?.build_pipeline}
        currentUserId={authStore.user?.id}
      />
    </section>

    <!-- Middle grid: Recent Projects + Inventory Alerts -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">

      <!-- Recent Projects -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Recent Projects</h3>
        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          {#if dashQuery.isPending}
            <div class="space-y-2">
              {#each [0, 1, 2] as _}<Skeleton class="h-9 rounded" />{/each}
            </div>
          {:else if dashQuery.data}
            {@const projects = dashQuery.data.recent_projects}
            {#if projects.length === 0}
              <p class="text-sm text-muted-foreground">No projects yet.</p>
            {:else}
              <div class="space-y-0.5">
                {#each projects as p (p.id)}
                  <a href={`/teams/${teamId}/projects/${p.id}`} use:linkAction
                    class="flex items-center justify-between px-2 py-1.5 rounded hover:bg-accent transition-colors no-underline gap-2">
                    <div class="flex items-baseline gap-1.5 min-w-0">
                      <span class="text-sm font-medium truncate">{p.name}</span>
                      {#if p.active_revision_label}
                        <span class="text-xs text-muted-foreground font-mono shrink-0">{p.active_revision_label}</span>
                      {/if}
                    </div>
                    <Badge variant={projectStatusVariant(p.status)} class="capitalize shrink-0">{p.status}</Badge>
                  </a>
                {/each}
              </div>
            {/if}
            <a href={`/teams/${teamId}/projects`} use:linkAction
              class="block text-xs text-muted-foreground hover:text-foreground transition-colors no-underline pt-1">
              View all projects →
            </a>
          {/if}
        </div>
      </section>

      <!-- Inventory Alerts -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Inventory Alerts</h3>
        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          {#if dashQuery.isPending}
            <div class="space-y-2">
              {#each [0, 1, 2] as _}<Skeleton class="h-9 rounded" />{/each}
            </div>
          {:else if dashQuery.data}
            {@const alerts = dashQuery.data.inventory_alerts}
            {#if alerts.length === 0}
              <p class="text-sm text-muted-foreground">All inventory levels are healthy.</p>
            {:else}
              <div class="space-y-0.5">
                {#each alerts as a (a.inventory_entry_id)}
                  <a href={`/teams/${teamId}/inventory/${a.inventory_entry_id}`} use:linkAction
                    class="flex items-center justify-between px-2 py-1.5 rounded hover:bg-accent transition-colors no-underline gap-2">
                    <span class="text-sm font-medium truncate">{a.mpn ?? a.description ?? '—'}</span>
                    <span class="text-xs text-muted-foreground shrink-0 tabular-nums">
                      {a.quantity_on_hand} / min {a.min_stock_level}
                    </span>
                  </a>
                {/each}
              </div>
            {/if}
            <button
              onclick={() => { setPartsStockStatus('low_stock'); navigate(`/teams/${teamId}/parts`); }}
              class="block text-xs text-muted-foreground hover:text-foreground transition-colors pt-1 text-left">
              View all in Inventory →
            </button>
          {/if}
        </div>
      </section>
    </div>

    <!-- Bottom grid: Outstanding Orders + Recent Activity -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">

      <!-- Outstanding Orders -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Outstanding Orders</h3>
        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          {#if dashQuery.isPending}
            <div class="space-y-2">
              {#each [0, 1] as _}<Skeleton class="h-9 rounded" />{/each}
            </div>
          {:else if dashQuery.data}
            {@const orders = dashQuery.data.outstanding_orders}
            {#if orders.length === 0}
              <p class="text-sm text-muted-foreground">No outstanding orders.</p>
            {:else}
              <div class="space-y-0.5">
                {#each orders as o (o.id)}
                  <a href={`/teams/${teamId}/orders/${o.id}`} use:linkAction
                    class="flex items-center justify-between px-2 py-1.5 rounded hover:bg-accent transition-colors no-underline gap-2">
                    <div class="min-w-0">
                      <div class="flex items-baseline gap-1.5">
                        <p class="text-sm font-medium truncate">{o.distributor_name}</p>
                        {#if o.purchase_order_number ?? o.external_reference}
                          <span class="text-xs text-muted-foreground font-mono shrink-0">#{o.purchase_order_number ?? o.external_reference}</span>
                        {/if}
                      </div>
                      <p class="text-xs text-muted-foreground">{o.item_count} item{o.item_count === 1 ? '' : 's'} · {formatRelative(o.created_at)}</p>
                    </div>
                    <Badge variant="outline" class="shrink-0 capitalize">{o.status.replace('_', ' ')}</Badge>
                  </a>
                {/each}
              </div>
            {/if}
            <a href={`/teams/${teamId}/orders`} use:linkAction
              class="block text-xs text-muted-foreground hover:text-foreground transition-colors no-underline pt-1">
              View all orders →
            </a>
          {/if}
        </div>
      </section>

      <!-- Recent Activity -->
      <section class="space-y-2">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Recent Activity</h3>
        <div class="rounded-lg border border-border bg-card overflow-hidden">
          {#if dashQuery.isPending}
            <div class="p-4 space-y-2">
              {#each [0, 1, 2] as _}<Skeleton class="h-9 rounded" />{/each}
            </div>
          {:else if dashQuery.data}
            {@const activity = dashQuery.data.recent_activity}
            {#if activity.length === 0}
              <p class="text-sm text-muted-foreground p-4">No recent activity.</p>
            {:else}
              {#each activity as item (item.id)}
                <ActivityEntry {item} {teamId} showActor />
              {/each}
            {/if}
          {/if}
        </div>
      </section>
    </div>

    <!-- Navigation Cards -->
    <section class="space-y-2">
      <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Navigate</h3>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {#each navCards as card}
          <a href={card.href()} use:linkAction
            class="rounded-lg border border-border bg-card p-4 hover:bg-accent transition-colors flex items-start gap-4 no-underline">
            <div class="mt-0.5 shrink-0 text-muted-foreground">
              <card.icon size={24} />
            </div>
            <div>
              <p class="font-semibold text-sm">{card.title}</p>
              <p class="text-xs text-muted-foreground mt-0.5">{card.description}</p>
            </div>
          </a>
        {/each}
      </div>
    </section>

  </div>
</div>

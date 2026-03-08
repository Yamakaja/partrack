<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import {
    Package, Warehouse, FolderOpen, ShoppingCart, ClipboardList,
    Hammer, PackageMinus, LogOut, PackageCheck,
  } from 'lucide-svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import Skeleton from '$lib/components/ui/skeleton/Skeleton.svelte';
  import { linkAction, navigate } from '$lib/router/index.svelte.js';
  import { getDashboard } from '$lib/api/dashboard.js';
  import { projectStatusVariant } from '$lib/types/projects';
  import { setPartsStockStatus } from '$lib/stores/navState.js';

  let { teamId }: { teamId: string } = $props();

  let dashQuery = createQuery(() => ({
    queryKey: ['dashboard', teamId],
    queryFn: () => getDashboard(teamId),
  }));

  function formatRelative(isoString: string): string {
    const diff = Date.now() - new Date(isoString).getTime();
    const minutes = Math.floor(diff / 60_000);
    const hours = Math.floor(diff / 3_600_000);
    const days = Math.floor(diff / 86_400_000);
    if (minutes < 2) return 'just now';
    if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'} ago`;
    if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`;
    if (days < 30) return `${days} day${days === 1 ? '' : 's'} ago`;
    const months = Math.floor(days / 30);
    return `${months} month${months === 1 ? '' : 's'} ago`;
  }

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

      {#if dashQuery.isPending}
        <div class="rounded-lg border border-border bg-card p-4">
          <div class="flex gap-3">
            {#each [0, 1, 2, 3] as _}
              <div class="flex-1 space-y-2">
                <Skeleton class="h-5 w-20 rounded" />
                <Skeleton class="h-12 rounded" />
                <Skeleton class="h-12 rounded" />
              </div>
            {/each}
          </div>
        </div>
      {:else if dashQuery.data}
        {@const pipeline = dashQuery.data.build_pipeline}
        {@const totalActive = pipeline.counts.draft + pipeline.counts.pending + pipeline.counts.ordered + pipeline.counts.ready}
        {@const stages = [
          { key: 'draft',   label: 'Draft' },
          { key: 'pending', label: 'Pending' },
          { key: 'ordered', label: 'Ordered' },
          { key: 'ready',   label: 'Ready' },
        ] as const}

        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          {#if totalActive === 0}
            <div class="flex flex-col items-center justify-center py-8 gap-2 text-muted-foreground">
              <Hammer size={28} />
              <p class="text-sm">No active builds.</p>
              <a href={`/teams/${teamId}/projects`} use:linkAction
                class="text-xs text-primary hover:underline no-underline">Start from a project →</a>
            </div>
          {:else}
            <div class="flex gap-1 items-start">
              {#each stages as stage, i}
                {#if i > 0}
                  <div class="text-muted-foreground/40 text-lg leading-none mt-6 shrink-0">›</div>
                {/if}
                <div class="flex-1 min-w-0 space-y-1.5">
                  <div class="flex items-center gap-1.5">
                    <span class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">{stage.label}</span>
                    {#if pipeline.counts[stage.key] > 0}
                      <span class="text-xs tabular-nums rounded-full px-1.5 py-0.5 leading-none
                        {stage.key === 'ready' ? 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-400' : 'bg-muted text-muted-foreground'}">
                        {pipeline.counts[stage.key]}
                      </span>
                    {/if}
                  </div>
                  <div class="space-y-1 max-h-48 overflow-y-auto">
                    {#if pipeline.stages[stage.key].length === 0}
                      <p class="text-xs text-muted-foreground/50 px-2 py-2 italic">—</p>
                    {:else}
                      {#each pipeline.stages[stage.key] as build (build.id)}
                        <a href={`/teams/${teamId}/builds/${build.id}`} use:linkAction
                          class="block px-2 py-1.5 rounded hover:bg-accent transition-colors no-underline min-w-0">
                          <div class="flex items-baseline justify-between gap-1 min-w-0">
                            <p class="text-xs font-medium truncate">{build.project_name} <span class="text-muted-foreground font-mono font-normal">{build.revision_label}</span></p>
                            <span class="text-xs text-muted-foreground shrink-0 tabular-nums">×{build.quantity}</span>
                          </div>
                          <p class="text-xs text-muted-foreground/70 truncate">{formatRelative(build.created_at)}{build.created_by_name ? ` · ${build.created_by_name}` : ''}</p>
                        </a>
                      {/each}
                    {/if}
                  </div>
                </div>
              {/each}
            </div>

            <!-- Summary line -->
            <p class="text-xs text-muted-foreground border-t border-border pt-2">
              {pipeline.counts.draft} draft · {pipeline.counts.pending} pending · {pipeline.counts.ordered} ordered ·
              {#if pipeline.counts.ready > 0}
                <span class="text-green-600 dark:text-green-400 font-medium">{pipeline.counts.ready} ready to assemble</span>
              {:else}
                0 ready
              {/if}
              <a href={`/teams/${teamId}/builds`} use:linkAction class="ml-2 hover:text-foreground transition-colors no-underline">View all →</a>
            </p>
          {/if}
        </div>
      {/if}
    </section>

    <!-- Middle grid: Recent Projects + Inventory Alerts -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">

      <!-- Recent Projects -->
      <section class="rounded-lg border border-border bg-card p-4 space-y-3">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Recent Projects</h3>

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
                  <span class="text-sm font-medium truncate">{p.name}</span>
                  <div class="flex items-center gap-2 shrink-0">
                    {#if p.active_revision_label}
                      <span class="text-xs text-muted-foreground font-mono">{p.active_revision_label}</span>
                    {/if}
                    <Badge variant={projectStatusVariant(p.status)} class="capitalize">{p.status}</Badge>
                  </div>
                </a>
              {/each}
            </div>
          {/if}
          <a href={`/teams/${teamId}/projects`} use:linkAction
            class="block text-xs text-muted-foreground hover:text-foreground transition-colors no-underline pt-1">
            View all projects →
          </a>
        {/if}
      </section>

      <!-- Inventory Alerts -->
      <section class="rounded-lg border border-border bg-card p-4 space-y-3">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Inventory Alerts</h3>

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
      </section>
    </div>

    <!-- Bottom grid: Outstanding Orders + Recent Activity -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">

      <!-- Outstanding Orders -->
      <section class="rounded-lg border border-border bg-card p-4 space-y-3">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Outstanding Orders</h3>

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
                    <p class="text-sm font-medium truncate">{o.distributor_name}</p>
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
      </section>

      <!-- Recent Activity -->
      <section class="rounded-lg border border-border bg-card p-4 space-y-3">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Recent Activity</h3>

        {#if dashQuery.isPending}
          <div class="space-y-2">
            {#each [0, 1, 2] as _}<Skeleton class="h-9 rounded" />{/each}
          </div>
        {:else if dashQuery.data}
          {@const activity = dashQuery.data.recent_activity}
          {#if activity.length === 0}
            <p class="text-sm text-muted-foreground">No recent activity.</p>
          {:else}
            <div class="space-y-0.5">
              {#each activity as item, idx (idx)}
                <div class="flex items-start gap-2 px-2 py-1.5">
                  <span class="mt-0.5 text-muted-foreground shrink-0">
                    {#if item.type === 'build'}
                      <Hammer size={14} />
                    {:else if item.type === 'checkout'}
                      <LogOut size={14} />
                    {:else}
                      <PackageCheck size={14} />
                    {/if}
                  </span>
                  <div class="min-w-0">
                    <p class="text-sm truncate">{item.summary}</p>
                    <p class="text-xs text-muted-foreground">{formatRelative(item.timestamp)}</p>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        {/if}
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

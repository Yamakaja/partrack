<script lang="ts">
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { Package, Archive, FolderOpen, ShoppingCart, ClipboardList, Hammer } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const cards = [
    {
      title: 'Parts Catalog',
      description: 'Browse and manage the global and team parts catalog.',
      icon: Package,
      href: () => `/teams/${teamId}/parts`,
    },
    {
      title: 'Inventory',
      description: 'Track stock levels, locations, and reorder thresholds.',
      icon: Archive,
      href: () => `/teams/${teamId}/inventory`,
    },
    {
      title: 'Projects',
      description: 'Manage PCB projects, BOMs, and shortfall checks.',
      icon: FolderOpen,
      href: () => `/teams/${teamId}/projects`,
    },
    {
      title: 'Cart',
      description: 'Review and manage parts staged for ordering.',
      icon: ShoppingCart,
      href: () => `/teams/${teamId}/cart`,
    },
    {
      title: 'Orders',
      description: 'View and track orders placed with distributors.',
      icon: ClipboardList,
      href: () => `/teams/${teamId}/orders`,
    },
    {
      title: 'Builds',
      description: 'Record PCB assembly events and stock deductions.',
      icon: Hammer,
      href: () => `/teams/${teamId}/builds`,
    },
  ];
</script>

<Topbar title="Dashboard" />

<div class="p-6 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
  {#each cards as card}
    <a
      href={card.href()}
      use:linkAction
      class="rounded-lg border border-border bg-card p-4 cursor-pointer hover:bg-accent transition-colors flex items-start gap-4 no-underline"
    >
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

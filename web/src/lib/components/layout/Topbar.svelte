<script lang="ts">
  import { type Snippet } from 'svelte';
  import { linkAction } from '$lib/router/index.svelte';
  import { Search } from 'lucide-svelte';
  import { paletteStore } from '$lib/stores/palette.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import HelpButton from '$lib/components/help/HelpButton.svelte';

  let {
    title,
    breadcrumbs = [],
    badges,
    actions,
  }: {
    title: string;
    breadcrumbs?: { label: string; href?: string }[];
    badges?: Snippet;
    actions?: Snippet;
  } = $props();

  let showSearch = $derived(!!activeTeamStore.current);
</script>

<div
  class="sticky top-0 z-10 grid items-center border-b border-border bg-background/95 backdrop-blur px-6 py-3"
  style="grid-template-columns: 1fr auto 1fr"
>
  <!-- Left: breadcrumbs + title -->
  <div class="flex items-center gap-2 text-lg leading-none min-w-0">
    {#if breadcrumbs.length > 0}
      <nav class="flex items-center gap-2 text-muted-foreground">
        {#each breadcrumbs as crumb, i}
          {#if i > 0}<span class="select-none">/</span>{/if}
          {#if crumb.href}
            <a href={crumb.href} use:linkAction class="hover:text-foreground transition-colors">{crumb.label}</a>
          {:else}
            <span>{crumb.label}</span>
          {/if}
        {/each}
      </nav>
      <span class="text-muted-foreground select-none">/</span>
    {/if}
    <h1 class="font-semibold truncate">{title}</h1>
    {#if badges}
      <div class="flex items-center gap-2 ml-5 shrink-0">
        {@render badges()}
      </div>
    {/if}
  </div>

  <!-- Center: search bar (only when inside a team) -->
  {#if showSearch}
    <button
      data-tour="search"
      onclick={() => { paletteStore.open = true; }}
      class="flex items-center gap-2 w-128 rounded-lg border border-border bg-muted/40 px-3 py-1.5 text-sm text-muted-foreground hover:bg-muted/70 hover:border-border/80 transition-colors"
    >
      <Search class="size-3.5 shrink-0" />
      <span class="flex-1 text-left">Search parts, projects...</span>
      <kbd class="hidden sm:inline-flex items-center rounded border border-border bg-background px-1.5 py-0.5 text-xs font-mono">⌘K</kbd>
    </button>
  {:else}
    <div></div>
  {/if}

  <!-- Right: actions -->
  <div class="flex items-center justify-end gap-2">
    {#if actions}
      {@render actions()}
    {/if}
    <HelpButton />
  </div>
</div>

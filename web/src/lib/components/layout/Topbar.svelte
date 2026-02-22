<script lang="ts">
  import { type Snippet } from 'svelte';

  let {
    title,
    breadcrumbs = [],
    actions,
  }: {
    title: string;
    breadcrumbs?: { label: string; href?: string }[];
    actions?: Snippet;
  } = $props();
</script>

<div class="sticky top-0 z-10 flex items-center justify-between border-b border-border bg-background/95 backdrop-blur px-6 py-3">
  <div>
    {#if breadcrumbs.length > 0}
      <nav class="flex items-center gap-1 text-xs text-muted-foreground mb-0.5">
        {#each breadcrumbs as crumb, i}
          {#if i > 0}<span>/</span>{/if}
          {#if crumb.href}
            <a href={crumb.href} class="hover:text-foreground transition-colors">{crumb.label}</a>
          {:else}
            <span>{crumb.label}</span>
          {/if}
        {/each}
      </nav>
    {/if}
    <h1 class="text-lg font-semibold leading-none">{title}</h1>
  </div>
  {#if actions}
    <div class="flex items-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>

<script lang="ts">
  import { type Snippet } from 'svelte';
  import { linkAction } from '$lib/router/index.svelte';

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
</script>

<div class="sticky top-0 z-10 flex items-center justify-between border-b border-border bg-background/95 backdrop-blur px-6 py-3">
  <div class="flex items-center gap-2 text-lg leading-none">
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
    <h1 class="font-semibold">{title}</h1>
    {#if badges}
      <div class="flex items-center gap-2 ml-5">
        {@render badges()}
      </div>
    {/if}
  </div>
  {#if actions}
    <div class="flex items-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>

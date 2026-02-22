<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    title,
    description = '',
    class: className = '',
    action,
  }: {
    title: string;
    description?: string;
    class?: string;
    action?: import('svelte').Snippet;
  } = $props();
</script>

<div class={cn('flex flex-col items-center justify-center py-16 text-center', className)}>
  <p class="text-sm font-medium text-foreground">{title}</p>
  {#if description}
    <p class="mt-1 text-sm text-muted-foreground">{description}</p>
  {/if}
  {#if action}
    <div class="mt-4">
      {@render action()}
    </div>
  {/if}
</div>

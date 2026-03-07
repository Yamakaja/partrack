<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    description?: string;
    titleClass?: string;
    badges?: Snippet;
    right?: Snippet;
    rightFull?: Snippet;
    children?: Snippet;
    footer?: Snippet;
  }

  let { title, description, titleClass, badges, right, rightFull, children, footer }: Props = $props();
</script>

<div class="rounded-lg border border-border bg-card p-4 space-y-3">
  <div class="flex gap-4 items-stretch">
    <div class="flex-1 space-y-3">
      <div class="flex items-start justify-between">
        <div>
          <div class="flex items-center gap-2 flex-wrap">
            <h2 class="text-xl font-bold {titleClass ?? ''}">{title}</h2>
            {@render badges?.()}
          </div>
          {#if description}
            <p class="text-sm text-muted-foreground mt-1">{description}</p>
          {/if}
        </div>
        {#if right}
          <div class="ml-4 shrink-0">
            {@render right()}
          </div>
        {/if}
      </div>

      {@render children?.()}

      {#if footer}
        <div class="pt-3 border-t border-border">
          {@render footer()}
        </div>
      {/if}
    </div>
    {#if rightFull}
      <div class="shrink-0">
        {@render rightFull()}
      </div>
    {/if}
  </div>
</div>

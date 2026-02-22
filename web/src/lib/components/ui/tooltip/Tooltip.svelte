<script lang="ts">
  import { Tooltip as TooltipPrimitive } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';

  let {
    content,
    class: className = '',
    children,
  }: {
    content: string;
    class?: string;
    children?: import('svelte').Snippet;
  } = $props();
</script>

<TooltipPrimitive.Provider>
  <TooltipPrimitive.Root>
    <TooltipPrimitive.Trigger>
      {@render children?.()}
    </TooltipPrimitive.Trigger>
    <TooltipPrimitive.Content
      class={cn(
        'z-50 overflow-hidden rounded-md bg-primary px-3 py-1.5 text-xs text-primary-foreground animate-in fade-in-0 zoom-in-95',
        className
      )}
    >
      {content}
    </TooltipPrimitive.Content>
  </TooltipPrimitive.Root>
</TooltipPrimitive.Provider>

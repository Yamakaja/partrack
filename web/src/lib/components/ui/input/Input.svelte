<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    class: className = '',
    value = $bindable(''),
    type = 'text',
    ...restProps
  }: {
    class?: string;
    value?: string | number;
    type?: string;
    [key: string]: any;
  } = $props();
</script>

<input
  {type}
  bind:value
  class={cn(
    'flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
    className
  )}
  {...restProps}
/>

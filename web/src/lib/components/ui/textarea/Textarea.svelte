<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    class: className = '',
    value = $bindable(''),
    ...restProps
  }: {
    class?: string;
    value?: string;
    [key: string]: any;
  } = $props();
</script>

<textarea
  bind:value
  class={cn(
    'flex min-h-20 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
    className
  )}
  {...restProps}
></textarea>

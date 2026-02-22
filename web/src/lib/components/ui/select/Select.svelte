<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    value = $bindable(''),
    class: className = '',
    disabled = false,
    children,
    ...restProps
  }: {
    value?: string;
    class?: string;
    disabled?: boolean;
    children?: import('svelte').Snippet;
    [key: string]: any;
  } = $props();
</script>

<select
  bind:value
  {disabled}
  class={cn(
    'flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
    className
  )}
  {...restProps}
>
  {@render children?.()}
</select>

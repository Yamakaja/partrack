<script lang="ts">
  import { Tabs as TabsPrimitive } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';

  let {
    value = $bindable(''),
    class: className = '',
    children,
    ...restProps
  }: {
    value?: string;
    class?: string;
    children?: import('svelte').Snippet;
    [key: string]: any;
  } = $props();
</script>

<TabsPrimitive.Root bind:value class={cn('', className)} {...restProps}>
  {@render children?.()}
</TabsPrimitive.Root>

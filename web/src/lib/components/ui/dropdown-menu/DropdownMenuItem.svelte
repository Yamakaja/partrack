<script lang="ts">
  import { DropdownMenu } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';

  let {
    class: className = '',
    disabled = false,
    onSelect,
    children,
  }: {
    class?: string;
    disabled?: boolean;
    onSelect?: () => void;
    children?: import('svelte').Snippet;
  } = $props();
</script>

<DropdownMenu.Item
  {disabled}
  onSelect={onSelect}
  class={cn(
    'relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none transition-colors focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
    className
  )}
>
  {@render children?.()}
</DropdownMenu.Item>

<script lang="ts">
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';

  let {
    open = $bindable(false),
    trigger,
    children,
    align = 'start',
  }: {
    open?: boolean;
    trigger?: import('svelte').Snippet;
    children?: import('svelte').Snippet;
    align?: 'start' | 'center' | 'end';
  } = $props();
</script>

<DropdownMenuPrimitive.Root bind:open>
  {#if trigger}
    <DropdownMenuPrimitive.Trigger>
      {@render trigger()}
    </DropdownMenuPrimitive.Trigger>
  {/if}
  <DropdownMenuPrimitive.Content
    {align}
    class="z-50 min-w-32 overflow-hidden rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95"
  >
    {@render children?.()}
  </DropdownMenuPrimitive.Content>
</DropdownMenuPrimitive.Root>

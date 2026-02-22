<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';

  let {
    open = $bindable(false),
    title,
    description,
    class: className = '',
    trigger,
    children,
  }: {
    open?: boolean;
    title?: string;
    description?: string;
    class?: string;
    trigger?: import('svelte').Snippet;
    children?: import('svelte').Snippet;
  } = $props();
</script>

<DialogPrimitive.Root bind:open>
  {#if trigger}
    <DialogPrimitive.Trigger>
      {@render trigger()}
    </DialogPrimitive.Trigger>
  {/if}
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay
      class="fixed inset-0 z-50 bg-black/80 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <DialogPrimitive.Content
      class={cn(
        'fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
        className
      )}
    >
      {#if title}
        <DialogPrimitive.Title class="text-lg font-semibold leading-none tracking-tight mb-2">
          {title}
        </DialogPrimitive.Title>
      {/if}
      {#if description}
        <DialogPrimitive.Description class="text-sm text-muted-foreground mb-4">
          {description}
        </DialogPrimitive.Description>
      {/if}
      {@render children?.()}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

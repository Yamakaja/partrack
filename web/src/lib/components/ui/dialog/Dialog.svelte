<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { cn } from '$lib/utils/cn.js';
  import type { Snippet } from 'svelte';

  let {
    open = $bindable(false),
    title,
    description,
    class: className = '',
    trigger,
    children,
    onConfirm,
    onCancel,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    confirmVariant = 'default',
  }: {
    open?: boolean;
    title?: string;
    description?: string;
    class?: string;
    trigger?: Snippet;
    children?: Snippet;
    onConfirm?: () => void;
    onCancel?: () => void;
    confirmLabel?: string;
    cancelLabel?: string;
    confirmVariant?: 'default' | 'destructive';
  } = $props();

  // Track whether we closed intentionally via confirm, so onOpenChange
  // can distinguish that from Esc / click-outside (which should call onCancel).
  let confirming = false;

  function handleCancel() {
    onCancel?.();
    open = false;
  }

  function handleConfirm() {
    confirming = true;
    onConfirm?.();
    open = false;
  }

  function handleOpenChange(v: boolean) {
    if (!v) {
      if (!confirming) onCancel?.();
      confirming = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key !== 'Enter' || !onConfirm) return;
    const tag = (e.target as HTMLElement).tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    e.preventDefault();
    handleConfirm();
  }
</script>

<DialogPrimitive.Root bind:open onOpenChange={handleOpenChange}>
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
      onkeydown={handleKeydown}
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
      {#if onConfirm}
        <div class="flex justify-end gap-2 mt-4">
          <Button variant="outline" onclick={handleCancel}>{cancelLabel}</Button>
          <Button variant={confirmVariant} onclick={handleConfirm}>{confirmLabel}</Button>
        </div>
      {/if}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

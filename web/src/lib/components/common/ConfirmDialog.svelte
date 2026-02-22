<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';

  let {
    open = $bindable(false),
    title = 'Are you sure?',
    description = 'This action cannot be undone.',
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    destructive = false,
    onconfirm,
    oncancel,
  }: {
    open?: boolean;
    title?: string;
    description?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    destructive?: boolean;
    onconfirm?: () => void;
    oncancel?: () => void;
  } = $props();

  function confirm() {
    open = false;
    onconfirm?.();
  }

  function cancel() {
    open = false;
    oncancel?.();
  }
</script>

<DialogPrimitive.Root bind:open>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <DialogPrimitive.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <DialogPrimitive.Title class="text-lg font-semibold">{title}</DialogPrimitive.Title>
      <DialogPrimitive.Description class="mt-2 text-sm text-muted-foreground">{description}</DialogPrimitive.Description>
      <div class="mt-4 flex justify-end gap-2">
        <Button variant="outline" onclick={cancel}>{cancelLabel}</Button>
        <Button variant={destructive ? 'destructive' : 'default'} onclick={confirm}>{confirmLabel}</Button>
      </div>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

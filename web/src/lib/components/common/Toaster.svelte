<script lang="ts">
  import { toasts, dismissToast } from '$lib/stores/toasts.svelte.js';
  import { X, CheckCircle, AlertCircle, Info } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';

  const icons = {
    success: CheckCircle,
    error: AlertCircle,
    info: Info,
  };

  const variants: Record<string, string> = {
    success: 'border-green-200 bg-green-50 text-green-900 dark:border-green-800 dark:bg-green-950 dark:text-green-100',
    error: 'border-destructive/30 bg-destructive/10 text-destructive',
    info: 'border-border bg-card text-foreground',
  };
</script>

<div class="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 w-80">
  {#each toasts as toast (toast.id)}
    {@const Icon = icons[toast.type]}
    <div
      class={cn(
        'flex items-start gap-3 rounded-lg border p-3 shadow-md text-sm',
        variants[toast.type]
      )}
    >
      <Icon class="h-4 w-4 mt-0.5 shrink-0" />
      <div class="flex-1 min-w-0">
        <p class="font-medium">{toast.message}</p>
        {#if toast.detail}
          <p class="mt-0.5 opacity-75 text-xs">{toast.detail}</p>
        {/if}
      </div>
      <button
        onclick={() => dismissToast(toast.id)}
        class="shrink-0 opacity-60 hover:opacity-100 transition-opacity"
      >
        <X class="h-4 w-4" />
      </button>
    </div>
  {/each}
</div>

<script lang="ts">
  import { toasts, dismissToast } from '$lib/stores/toasts.svelte.js';
  import { X, CircleCheck, CircleAlert, Info, TriangleAlert } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';

  let { position = 'bottom-right' }: { position?: 'top-right' | 'bottom-right' } = $props();

  const icons = {
    success: CircleCheck,
    error: CircleAlert,
    info: Info,
    warning: TriangleAlert,
  };

  const variants: Record<string, string> = {
    success: 'border-success/30 bg-success-subtle text-success',
    error: 'border-destructive/30 bg-destructive/10 text-destructive',
    info: 'border-border bg-card text-foreground',
    warning: 'border-warning/30 bg-warning-subtle text-warning',
  };
</script>

<div class={cn('fixed right-4 z-[100] flex flex-col gap-2 w-80', position === 'top-right' ? 'top-4' : 'bottom-4')}>
  {#each toasts as toast (toast.id)}
    {@const Icon = icons[toast.type]}
    <div
      class={cn(
        'flex items-start gap-3 rounded-lg border p-3 shadow-md text-sm',
        variants[toast.type]
      )}
    >
      <Icon class={cn('h-4 w-4 mt-0.5 shrink-0', toast.progress && toast.progress.total === 0 && 'animate-spin')} />
      <div class="flex-1 min-w-0">
        <p class="font-medium">{toast.message}</p>
        {#if toast.detail}
          <p class="mt-0.5 opacity-75 text-xs">{toast.detail}</p>
        {/if}
        {#if toast.progress}
          {@const pct = toast.progress.total > 0
            ? Math.min(100, Math.round((toast.progress.current / toast.progress.total) * 100))
            : 0}
          <div class="mt-2 flex items-center gap-2">
            <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-current/15">
              {#if toast.progress.total > 0}
                <div class="h-full rounded-full bg-current transition-[width] duration-300" style="width: {pct}%"></div>
              {:else}
                <div class="h-full w-1/3 animate-pulse rounded-full bg-current"></div>
              {/if}
            </div>
            {#if toast.progress.total > 0}
              <span class="shrink-0 text-xs tabular-nums opacity-75">{toast.progress.current}/{toast.progress.total}</span>
            {/if}
          </div>
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

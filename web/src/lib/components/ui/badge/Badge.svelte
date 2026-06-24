<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    variant = 'default',
    class: className = '',
    children,
  }: {
    variant?: 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning' | 'danger' | 'attention' | 'info' | 'reserved';
    class?: string;
    children?: import('svelte').Snippet;
  } = $props();

  const base = 'inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors';

  const variants: Record<string, string> = {
    default: 'border-transparent bg-primary text-primary-foreground',
    secondary: 'border-transparent bg-secondary text-secondary-foreground',
    destructive: 'border-transparent bg-destructive text-destructive-foreground',
    outline: 'text-foreground',
    success: 'border-transparent bg-success-subtle text-success',
    warning: 'border-transparent bg-warning-subtle text-warning',
    danger: 'border-transparent bg-danger-subtle text-danger',
    attention: 'border-transparent bg-attention-subtle text-attention',
    info: 'border-transparent bg-info-subtle text-info',
    reserved: 'border-transparent bg-reserved-subtle text-reserved',
  };
</script>

<span class={cn(base, variants[variant], className)}>
  {@render children?.()}
</span>

<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    variant = 'default',
    class: className = '',
    children,
  }: {
    variant?: 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning';
    class?: string;
    children?: import('svelte').Snippet;
  } = $props();

  const base = 'inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors';

  const variants: Record<string, string> = {
    default: 'border-transparent bg-primary text-primary-foreground',
    secondary: 'border-transparent bg-secondary text-secondary-foreground',
    destructive: 'border-transparent bg-destructive text-destructive-foreground',
    outline: 'text-foreground',
    success: 'border-transparent bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200',
    warning: 'border-transparent bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200',
  };
</script>

<span class={cn(base, variants[variant], className)}>
  {@render children?.()}
</span>

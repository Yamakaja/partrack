<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '', children, ...restProps }: { class?: string; children?: import('svelte').Snippet; [key: string]: unknown } = $props();
</script>
<th {...restProps} class={cn('h-10 px-4 text-left align-middle font-medium text-muted-foreground', className)}>{@render children?.()}</th>

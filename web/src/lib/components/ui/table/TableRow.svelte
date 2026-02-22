<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '', children, ...restProps }: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>
<tr class={cn('border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted', className)} {...restProps}>{@render children?.()}</tr>

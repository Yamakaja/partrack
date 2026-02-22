<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '', children, ...restProps }: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>
<td class={cn('p-4 align-middle', className)} {...restProps}>{@render children?.()}</td>

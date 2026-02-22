<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>
<tbody class={cn('[&_tr:last-child]:border-0', className)}>{@render children?.()}</tbody>

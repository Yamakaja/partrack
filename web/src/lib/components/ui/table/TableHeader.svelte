<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>
<thead class={cn('[&_tr]:border-b', className)}>{@render children?.()}</thead>

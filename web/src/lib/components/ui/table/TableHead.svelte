<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>
<th class={cn('h-10 px-4 text-left align-middle font-medium text-muted-foreground', className)}>{@render children?.()}</th>

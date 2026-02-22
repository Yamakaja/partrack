<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  let {
    class: className = '',
    children,
  }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<div class="relative w-full overflow-auto">
  <table class={cn('w-full caption-bottom text-sm', className)}>
    {@render children?.()}
  </table>
</div>

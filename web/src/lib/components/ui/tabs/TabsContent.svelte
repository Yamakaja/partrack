<script lang="ts">
  import { Tabs } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';
  let { value, class: className = '', children }: { value: string; class?: string; children?: import('svelte').Snippet } = $props();
</script>
<Tabs.Content
  {value}
  class={cn('mt-2 ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', className)}
>
  {@render children?.()}
</Tabs.Content>

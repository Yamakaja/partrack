<script lang="ts">
  import { DropdownMenu } from 'bits-ui';
  import { cn } from '$lib/utils/cn.js';
  let { class: className = '' }: { class?: string } = $props();
</script>
<DropdownMenu.Separator class={cn('-mx-1 my-1 h-px bg-border', className)} />

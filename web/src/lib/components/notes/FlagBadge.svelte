<script lang="ts">
  import { FLAG_META } from './flags.js';
  import type { PartFlag } from '$lib/api/notes.js';

  let { flag }: { flag: PartFlag } = $props();
  let meta = $derived(FLAG_META[flag]);
  let Icon = $derived(meta.icon);
</script>

{#if flag !== 'none'}
  <span class="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs font-medium {meta.badgeClass}">
    {#if Icon}<Icon class="h-3 w-3" />{/if}
    {meta.label}
  </span>
{/if}

<script lang="ts">
  import { AlertTriangle } from 'lucide-svelte';

  let { skus }: { skus: string[] } = $props();
</script>

{#if skus.length > 0}
  <div class="mt-3 rounded-md border border-warning/30 bg-warning-subtle p-3">
    <p class="text-sm font-medium flex items-center gap-1.5 text-warning mb-1.5">
      <AlertTriangle class="h-4 w-4 shrink-0" />
      {skus.length} SKU{skus.length !== 1 ? 's' : ''} could not be resolved
    </p>
    <ul class="text-xs text-warning font-mono space-y-0.5 max-h-28 overflow-y-auto">
      {#each skus.slice(0, 10) as sku}
        <li>{sku}</li>
      {/each}
      {#if skus.length > 10}
        <li class="font-sans text-xs">… and {skus.length - 10} more</li>
      {/if}
    </ul>
  </div>
{/if}

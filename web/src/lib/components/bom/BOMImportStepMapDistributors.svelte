<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import type { Distributor } from '$lib/types/parts.js';
  import type { DistMapping } from './bom-import-utils.js';

  let {
    uniqueDistributorNames,
    availableDistributors,
    distributorMappings = $bindable(),
    hasRequiredColumns,
    onback,
    onnext,
    oncancel,
  }: {
    uniqueDistributorNames: string[];
    availableDistributors: Distributor[];
    distributorMappings: DistMapping;
    hasRequiredColumns: boolean;
    onback: () => void;
    onnext: () => void;
    oncancel: () => void;
  } = $props();
</script>

<div class="min-h-0 flex-1 overflow-auto px-6 py-4">
  {#if uniqueDistributorNames.length === 0}
    <p class="py-8 text-center text-sm text-muted-foreground">
      No distributors detected in the BOM.
    </p>
  {:else}
    <div class="space-y-4">
      <div class="overflow-x-auto">
        <table class="w-full border-collapse text-xs">
          <thead>
            <tr class="border-b border-border">
              <th class="px-3 py-1.5 text-left font-semibold text-muted-foreground">CSV distributor</th>
              <th class="px-3 py-1.5 text-left font-semibold text-muted-foreground">Resolve using</th>
            </tr>
          </thead>
          <tbody>
            {#each uniqueDistributorNames as name (name)}
              <tr class="border-b border-border/60">
                <td class="px-3 py-1.5 font-mono text-xs">{name}</td>
                <td class="px-3 py-1.5">
                  <select
                    bind:value={distributorMappings[name]}
                    class="w-full rounded border border-input bg-background px-1.5 py-0.5 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    <option value={''}>None (do not use for resolution)</option>
                    {#each availableDistributors as d}
                      <option value={d.id}>{d.name}</option>
                    {/each}
                  </select>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>

<!-- Footer -->
<div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
  <Button variant="outline" onclick={onback}>← Back</Button>
  <div class="flex gap-2">
    <Button variant="outline" onclick={oncancel}>Cancel</Button>
    <Button onclick={onnext} disabled={!hasRequiredColumns}>
      Next: Details →
    </Button>
  </div>
</div>

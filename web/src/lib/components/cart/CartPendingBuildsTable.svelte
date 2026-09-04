<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { toggleBuildCartInclusion } from '$lib/api/builds.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ExternalLink } from 'lucide-svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import type { Build } from '$lib/types/orders.js';

  let { teamId, pendingBuilds, buildOrderCosts, canEdit = true }: {
    teamId: string;
    pendingBuilds: Build[];
    buildOrderCosts: Map<string, number>;
    /** Toggling cart inclusion is purchaser+; false renders the checkbox inert. */
    canEdit?: boolean;
  } = $props();

  const qc = useQueryClient();
  let togglingBuilds = $state<Set<string>>(new Set());

  async function handleToggleInclusion(buildId: string, newExcluded: boolean) {
    togglingBuilds = new Set([...togglingBuilds, buildId]);
    try {
      await toggleBuildCartInclusion(teamId, buildId, newExcluded);
      qc.invalidateQueries({ queryKey: ['builds', teamId, 'pending'] });
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
    } catch (err: any) {
      pushToast('error', 'Failed to update build inclusion', err.message);
    } finally {
      const s = new Set(togglingBuilds);
      s.delete(buildId);
      togglingBuilds = s;
    }
  }
</script>

{#if pendingBuilds.length === 0}
  <p class="text-sm text-muted-foreground">
    No pending builds.
    <button class="underline" onclick={() => navigate(`/teams/${teamId}/builds`)}>Create a build plan</button>
    and submit for fulfilment to drive cart demand.
  </p>
{:else}
  <Table>
    <TableHeader>
      <TableRow>
        <TableHead class="w-16">Include</TableHead>
        <TableHead>Project</TableHead>
        <TableHead>Revision</TableHead>
        <TableHead>Quantity</TableHead>
        <TableHead>Est. Order Cost</TableHead>
        <TableHead class="w-10"></TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {#each pendingBuilds as build (build.id)}
        {@const excluded = build.excluded_from_cart ?? false}
        {@const estCost = buildOrderCosts.get(build.id)}
        <TableRow>
          <TableCell>
            <input
              type="checkbox"
              checked={!excluded}
              disabled={!canEdit || togglingBuilds.has(build.id)}
              title={canEdit ? undefined : 'Requires the purchaser role'}
              onchange={(e) => handleToggleInclusion(build.id, !(e.target as HTMLInputElement).checked)}
              class="h-4 w-4 {canEdit ? 'cursor-pointer' : 'cursor-not-allowed'}"
            />
          </TableCell>
          <TableCell class="text-sm font-medium">{build.project_name}</TableCell>
          <TableCell class="font-mono text-xs text-muted-foreground">{build.bom_revision_label}</TableCell>
          <TableCell class="tabular-nums">{build.quantity}</TableCell>
          <TableCell class="tabular-nums text-sm">
            {#if estCost !== undefined && estCost > 0}
              <span title="Estimated parts ordering cost based on quantities and prices in the current cart">
                <PriceDisplay value={estCost.toFixed(2)} />
              </span>
            {:else}
              <span class="text-muted-foreground">—</span>
            {/if}
          </TableCell>
          <TableCell>
            <Button
              variant="ghost"
              size="sm"
              onclick={() => navigate(`/teams/${teamId}/builds/${build.id}`)}
              class="text-muted-foreground"
            >
              <ExternalLink class="h-4 w-4" />
            </Button>
          </TableCell>
        </TableRow>
      {/each}
    </TableBody>
  </Table>
{/if}

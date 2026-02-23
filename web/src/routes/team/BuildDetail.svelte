<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { getBuild } from '$lib/api/builds.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';

  let { teamId, buildId }: { teamId: string; buildId: string } = $props();

  let buildQuery = createQuery(() => ({
    queryKey: ['build', buildId],
    queryFn: () => getBuild(teamId, buildId),
  }));
</script>

<div class="flex flex-col h-full">
  {#if buildQuery.isPending}
    <Topbar title="Build" />
    <div class="p-6 text-sm text-muted-foreground">Loading build…</div>
  {:else if buildQuery.isError}
    <Topbar title="Build" />
    <div class="p-6 text-sm text-destructive">Error: {buildQuery.error.message}</div>
  {:else if buildQuery.data}
    {@const build = buildQuery.data}
    <Topbar
      title={build.project_name}
      breadcrumbs={[
        { label: 'Builds', href: `/teams/${teamId}/builds` },
        { label: build.project_name },
      ]}
    />

    <div class="flex-1 overflow-auto p-6 space-y-4">
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-4 text-sm">
        <div>
          <p class="text-muted-foreground text-xs mb-0.5">Project</p>
          <p class="font-medium">{build.project_name}</p>
        </div>
        <div>
          <p class="text-muted-foreground text-xs mb-0.5">Revision</p>
          <p class="font-medium">{build.bom_revision_label}</p>
        </div>
        <div>
          <p class="text-muted-foreground text-xs mb-0.5">Qty Built</p>
          <p class="font-medium">{build.quantity_built}</p>
        </div>
        <div>
          <p class="text-muted-foreground text-xs mb-0.5">Built At</p>
          <DateDisplay value={build.built_at} />
        </div>
        <div>
          <p class="text-muted-foreground text-xs mb-0.5">Built By</p>
          <p class="font-medium">{build.built_by_display_name}</p>
        </div>
        {#if build.notes}
          <div class="col-span-2">
            <p class="text-muted-foreground text-xs mb-0.5">Notes</p>
            <p class="font-medium">{build.notes}</p>
          </div>
        {/if}
      </div>

      <h2 class="text-sm font-semibold pt-2">Consumed Parts</h2>
      {#if build.items && build.items.length > 0}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Part MPN</TableHead>
              <TableHead>Ref Designators</TableHead>
              <TableHead>Qty Deducted</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each build.items as item (item.bom_line_item_id)}
              <TableRow>
                <TableCell class="font-mono font-medium text-xs">{item.part_mpn ?? '—'}</TableCell>
                <TableCell class="text-muted-foreground text-sm">{item.reference_designators}</TableCell>
                <TableCell>{item.quantity_deducted}</TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {:else}
        <div class="text-sm text-muted-foreground">No consumed parts recorded.</div>
      {/if}
    </div>
  {/if}
</div>

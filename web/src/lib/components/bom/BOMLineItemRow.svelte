<script lang="ts">
  import type { BOMLineItem } from '$lib/types/projects.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import PartSearch from '$lib/components/parts/PartSearch.svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { TableRow, TableCell } from '$lib/components/ui/table/index.js';

  let {
    item,
    teamId,
    onresolve,
  }: {
    item: BOMLineItem;
    teamId: string;
    onresolve?: (itemId: string, partId: string | null) => void;
  } = $props();

  let selectedPart = $state<PartSummary | null>(null);

  function handleSelect(part: PartSummary) {
    selectedPart = part;
    onresolve?.(item.id, part.id);
  }

  function handleClear() {
    selectedPart = null;
    onresolve?.(item.id, null);
  }
</script>

<TableRow>
  <TableCell class="font-mono text-xs text-muted-foreground">
    {item.reference_designators || '—'}
  </TableCell>
  <TableCell class="text-center tabular-nums">{item.quantity}</TableCell>
  <TableCell class="max-w-xs truncate text-muted-foreground">
    {item.raw_description ?? '—'}
  </TableCell>
  <TableCell>
    {#if item.resolved_part_id}
      <Badge variant="success">Resolved</Badge>
      {#if item.resolved_part_mpn}
        <span class="ml-2 font-mono text-xs">{item.resolved_part_mpn}</span>
      {/if}
    {:else}
      <Badge variant="outline">Unresolved</Badge>
    {/if}
  </TableCell>
  <TableCell class="w-64">
    {#if item.resolved_part_id && !selectedPart}
      <div class="flex items-center gap-2">
        <span class="font-mono text-xs text-muted-foreground">{item.resolved_part_mpn ?? item.resolved_part_id}</span>
        <button
          type="button"
          class="text-xs text-muted-foreground hover:text-destructive transition-colors"
          onclick={handleClear}
        >
          Clear
        </button>
      </div>
    {:else}
      <PartSearch
        {teamId}
        bind:value={selectedPart}
        placeholder="Search to resolve…"
        onselect={handleSelect}
      />
    {/if}
  </TableCell>
</TableRow>

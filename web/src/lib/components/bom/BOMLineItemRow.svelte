<script lang="ts">
  import type { BOMLineItem } from '$lib/types/projects.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { TableRow, TableCell } from '$lib/components/ui/table/index.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import FlagBadge from '$lib/components/notes/FlagBadge.svelte';

  const COLUMN_WIDTH: number = 80

  function truncateRow(s: string|undefined): string {
    if (!s)
      return "";
    if (s.length <= COLUMN_WIDTH)
      return s;

    return s.substring(0, COLUMN_WIDTH) + "..."
  }

  let {
    item,
    teamId,
    onstatusclick,
  }: {
    item: BOMLineItem;
    teamId?: string;
    onstatusclick?: (itemId: string) => void;
  } = $props();

  type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning';

  let statusBadge = $derived((): { label: string; variant: BadgeVariant } => {
    if (item.ignored) return { label: 'Ignored', variant: 'secondary' };
    if (!item.resolved_part_id) {
      if (item.resolution_state === 'ambiguous') return { label: 'Ambiguous', variant: 'warning' };
      return { label: 'Unresolved', variant: 'outline' };
    }
    if (item.resolution_state === 'auto_sku') return { label: 'Auto (SKU)', variant: 'success' };
    if (item.resolution_state === 'auto_mpn') return { label: 'Auto (MPN)', variant: 'success' };
    if (item.resolution_state === 'auto_generic') return { label: 'Auto (Generic)', variant: 'success' };
    if (item.resolution_state === 'auto_prior_revision') return { label: 'Auto (Prior)', variant: 'success' };
    if (item.resolution_state === 'manual') return { label: 'Manual', variant: 'success' };
    return { label: 'Resolved', variant: 'success' };
  });
</script>

<TableRow class={item.ignored ? 'opacity-50' : ''}>
  <TableCell>
    {item.part_name || ''}
  </TableCell>
  <TableCell>
    {item.manufacturer || ''}
  </TableCell>
  <TableCell>
    {item.mpn || ''}
  </TableCell>
  <TableCell class="max-w-xs truncate text-muted-foreground">
    {item.raw_description ?? ''}
  </TableCell>
  <TableCell class="font-mono text-xs text-muted-foreground">
    {truncateRow(item.reference_designators) || '—'}
  </TableCell>
  <TableCell>
    <div class="flex items-center gap-1">
      <span>{item.distributor_name || ''}</span>
      {#if item.distributor_name}
        <span
          class="text-xs align-middle"
          title={item.has_linked_distributor ? 'Distributor linked' : 'Distributor not linked'}
        >
          {item.has_linked_distributor ? '✓' : '✕'}
        </span>
      {/if}
    </div>
  </TableCell>
  <TableCell>
    {item.sku || ''}
  </TableCell>
  <TableCell class="text-center tabular-nums">{item.quantity}</TableCell>
  <TableCell>
    {@const badge = statusBadge()}
    {#if onstatusclick}
      <button
        type="button"
        class="cursor-pointer hover:opacity-75 transition-opacity inline-flex items-center gap-1.5"
        onclick={() => onstatusclick?.(item.id)}
        title="Open review"
      >
        <Badge variant={badge.variant}>{badge.label}</Badge>
        {#if item.resolved_part_id && item.resolved_part_mpn}
          {#if teamId}
            <a
              use:linkAction
              href="/teams/{teamId}/parts/{item.resolved_part_id}"
              onclick={(e) => e.stopPropagation()}
              class="font-mono text-xs text-muted-foreground hover:underline"
            >{item.resolved_part_mpn}</a>
          {:else}
            <span class="font-mono text-xs text-muted-foreground">{item.resolved_part_mpn}</span>
          {/if}
          {#if item.resolved_part_flag}<FlagBadge flag={item.resolved_part_flag} />{/if}
        {/if}
      </button>
    {:else}
      <div class="inline-flex items-center gap-1.5">
        <Badge variant={badge.variant}>{badge.label}</Badge>
        {#if item.resolved_part_id && item.resolved_part_mpn}
          {#if teamId}
            <a
              use:linkAction
              href="/teams/{teamId}/parts/{item.resolved_part_id}"
              class="font-mono text-xs text-muted-foreground hover:underline"
            >{item.resolved_part_mpn}</a>
          {:else}
            <span class="font-mono text-xs text-muted-foreground">{item.resolved_part_mpn}</span>
          {/if}
          {#if item.resolved_part_flag}<FlagBadge flag={item.resolved_part_flag} />{/if}
        {/if}
      </div>
    {/if}
  </TableCell>
</TableRow>

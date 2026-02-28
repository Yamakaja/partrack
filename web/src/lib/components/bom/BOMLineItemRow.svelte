<script lang="ts">
  import type { BOMLineItem } from '$lib/types/projects.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import type { SubstituteItem } from '$lib/api/revisions.js';
  import PartSearch from '$lib/components/parts/PartSearch.svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { TableRow, TableCell } from '$lib/components/ui/table/index.js';

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
    projectId,
    revisionId,
    substitutes = [],
    onresolve,
    onignore,
    onsubstitutechange,
  }: {
    item: BOMLineItem;
    teamId: string;
    projectId: string;
    revisionId: string;
    substitutes?: SubstituteItem[];
    onresolve?: (itemId: string, partId: string | null) => void;
    onignore?: (itemId: string, ignored: boolean) => void;
    onsubstitutechange?: () => void;
  } = $props();

  let selectedPart = $state<PartSummary | null>(null);
  let addingSubstitute = $state(false);
  let showSubstituteSearch = $state(false);

  function handleSelect(part: PartSummary) {
    selectedPart = part;
    onresolve?.(item.id, part.id);
  }

  function handleClear() {
    selectedPart = null;
    onresolve?.(item.id, null);
  }

  async function handleAddSubstitute(part: PartSummary) {
    addingSubstitute = true;
    showSubstituteSearch = false;
    try {
      const { addSubstitute } = await import('$lib/api/revisions.js');
      await addSubstitute(teamId, projectId, revisionId, item.id, part.id);
      onsubstitutechange?.();
    } catch {
      // ignore duplicate errors silently
    } finally {
      addingSubstitute = false;
    }
  }

  async function handleRemoveSubstitute(partId: string) {
    try {
      const { removeSubstitute } = await import('$lib/api/revisions.js');
      await removeSubstitute(teamId, projectId, revisionId, item.id, partId);
      onsubstitutechange?.();
    } catch {
      // ignore
    }
  }

  type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning';

  let statusBadge = $derived((): { label: string; variant: BadgeVariant } => {
    if (item.ignored) return { label: 'Ignored', variant: 'secondary' };
    if (!item.resolved_part_id) {
      if (item.resolution_state === 'ambiguous') return { label: 'Ambiguous', variant: 'warning' };
      return { label: 'Unresolved', variant: 'outline' };
    }
    if (item.resolution_state === 'auto_sku') return { label: 'Auto (SKU)', variant: 'success' };
    if (item.resolution_state === 'auto_mpn') return { label: 'Auto (MPN)', variant: 'success' };
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
    <Badge variant={badge.variant}>{badge.label}</Badge>
    {#if item.resolved_part_id && item.resolved_part_mpn}
      <span class="ml-2 font-mono text-xs">{item.resolved_part_mpn}</span>
    {/if}
  </TableCell>
  <TableCell class="w-64">
    {#if item.ignored}
      <button
        type="button"
        class="text-xs text-muted-foreground hover:text-foreground transition-colors"
        onclick={() => onignore?.(item.id, false)}
      >
        Un-ignore
      </button>
    {:else}
      <div class="flex flex-col gap-1">
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
        <button
          type="button"
          class="text-xs text-muted-foreground hover:text-amber-600 transition-colors text-left"
          onclick={() => onignore?.(item.id, true)}
        >
          Ignore
        </button>
      </div>
    {/if}
  </TableCell>
  <TableCell class="min-w-48">
    {#if !item.ignored}
      <div class="flex flex-wrap items-center gap-1">
        {#each substitutes as sub (sub.part_id)}
          <span class="inline-flex items-center gap-0.5 rounded-full bg-muted px-2 py-0.5 text-xs font-mono">
            {sub.part_mpn || sub.part_id.slice(0, 8)}
            <button
              type="button"
              class="ml-0.5 text-muted-foreground hover:text-destructive transition-colors"
              onclick={() => handleRemoveSubstitute(sub.part_id)}
              aria-label="Remove substitute"
            >×</button>
          </span>
        {/each}
        {#if !showSubstituteSearch}
          <button
            type="button"
            class="text-xs text-muted-foreground hover:text-foreground transition-colors"
            onclick={() => { showSubstituteSearch = true; }}
            disabled={addingSubstitute}
          >
            + sub
          </button>
        {:else}
          <div class="w-40">
            <PartSearch
              {teamId}
              placeholder="Add substitute…"
              onselect={(p) => handleAddSubstitute(p)}
            />
          </div>
          <button
            type="button"
            class="text-xs text-muted-foreground hover:text-destructive transition-colors"
            onclick={() => { showSubstituteSearch = false; }}
          >Cancel</button>
        {/if}
      </div>
    {/if}
  </TableCell>
</TableRow>

<script lang="ts">
  import type { ActivityEntry } from '$lib/api/activityLog.js';
  import { renderSegments, actionIcon } from './templates.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { formatRelative } from '$lib/utils/date.js';

  let {
    item,
    teamId,
    showActor = false,
    onAdjust,
  }: {
    item: ActivityEntry;
    teamId: string;
    showActor?: boolean;
    onAdjust?: (entryId: string, qty: number | null, mpn: string | null, manufacturer: string | null) => void;
  } = $props();

  const isNotFound = $derived(item.action === 'scan.part_not_found');
  const segments = $derived(renderSegments(item, teamId));
</script>

<div
  class="flex gap-3 px-4 py-3 border-b border-border transition-colors
    {isNotFound ? 'bg-warning-subtle' : 'hover:bg-accent/50'}"
>
  <!-- Icon / part thumbnail -->
  <div class="shrink-0 w-10 h-10 rounded-full overflow-hidden flex items-center justify-center text-xs
    {isNotFound
      ? 'bg-warning-subtle text-warning'
      : 'bg-muted text-muted-foreground'}">
    {#if item.part_image_url}
      <img src={item.part_image_url} alt="" class="w-full h-full object-contain" />
    {:else}
      {actionIcon(item.action)}
    {/if}
  </div>

  <!-- Content -->
  <div class="flex-1 min-w-0">
    <div class="flex items-start justify-between gap-2">
      <span class="text-xs {isNotFound ? 'text-warning' : 'text-foreground'}">
        {#if showActor && item.actor_label}<span class="font-medium">{item.actor_label}</span>{' '}{/if}
        {#each segments as seg}
          {#if seg.kind === 'text'}{seg.text}
          {:else if seg.url}<a href={seg.url} use:linkAction class="font-medium hover:underline">{seg.label}</a>
          {:else}<span class="font-medium">{seg.label}</span>{/if}
        {/each}
      </span>
      <span class="text-[10px] text-muted-foreground shrink-0 whitespace-nowrap">{formatRelative(item.created_at)}</span>
    </div>

    {#if item.part_manufacturer}
      <p class="text-[10px] text-muted-foreground truncate mt-0.5">{item.part_manufacturer}</p>
    {/if}

    {#if isNotFound || (onAdjust && item.inventory_entry_id)}
      <div class="mt-1.5 flex items-center gap-2">
        {#if isNotFound}
          <span class="text-[10px] text-warning">
            {item.subject ? 'Resolved ✓' : 'Unresolved'}
          </span>
        {/if}
        {#if onAdjust && item.inventory_entry_id}
          <button
            onclick={() => onAdjust(item.inventory_entry_id!, item.inventory_quantity_on_hand, item.subject?.label ?? null, item.part_manufacturer)}
            class="text-[10px] px-2 py-0.5 rounded border border-border bg-muted text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            ± Adjust
          </button>
        {/if}
      </div>
    {/if}
  </div>
</div>

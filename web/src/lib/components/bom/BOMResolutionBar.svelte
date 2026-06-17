<script lang="ts">
  import type { BOMLineItem } from '$lib/types/projects.js';

  let { items }: { items: BOMLineItem[] } = $props();

  type Bucket = {
    key: string;
    label: string;
    color: string;
    count: number;
    animate?: boolean;
  };

  // Classify a line item into one progress bucket.
  function bucketOf(i: BOMLineItem): string {
    if (i.ignored) return 'ignored';
    const s = i.resolution_state;
    if (s === 'auto_sku' || s === 'auto_mpn' || s === 'auto_generic' || s === 'auto_prior_revision') return 'existing';
    if (s === 'manual') return 'manual';
    if (s === 'auto_import') return 'newAuto';
    if (s === 'import_ambiguous' || s === 'ambiguous') return 'ambiguous';
    if (s === 'import_no_result') return 'noResult';
    if (s === 'importing') return 'importing';
    if (i.resolved_part_id) return 'existing';
    return 'unresolved';
  }

  const buckets = $derived.by<Bucket[]>(() => {
    const counts: Record<string, number> = {};
    for (const i of items) counts[bucketOf(i)] = (counts[bucketOf(i)] ?? 0) + 1;
    const defs: Bucket[] = [
      { key: 'existing',   label: 'Existing parts',     color: 'bg-green-500',   count: counts.existing   ?? 0 },
      { key: 'newAuto',    label: 'New parts imported',  color: 'bg-emerald-400', count: counts.newAuto    ?? 0 },
      { key: 'manual',     label: 'Resolved manually',   color: 'bg-sky-500',     count: counts.manual     ?? 0 },
      { key: 'importing',  label: 'Importing…',          color: 'bg-indigo-400',  count: counts.importing  ?? 0, animate: true },
      { key: 'ambiguous',  label: 'Needs review',        color: 'bg-amber-500',   count: counts.ambiguous  ?? 0 },
      { key: 'noResult',   label: 'No result',           color: 'bg-destructive', count: counts.noResult   ?? 0 },
      { key: 'unresolved', label: 'Unresolved',          color: 'bg-muted-foreground/40', count: counts.unresolved ?? 0 },
      { key: 'ignored',    label: 'Ignored',             color: 'bg-muted-foreground/20', count: counts.ignored ?? 0 },
    ];
    return defs.filter((b) => b.count > 0);
  });

  const total = $derived(items.length);
  const importing = $derived(items.filter((i) => bucketOf(i) === 'importing').length);
</script>

{#if total > 0}
  <div class="rounded-lg border border-border bg-card p-4 space-y-3">
    <div class="flex items-center justify-between">
      <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Resolution</h3>
      {#if importing > 0}
        <span class="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span class="h-2 w-2 animate-pulse rounded-full bg-indigo-400"></span>
          {importing} importing…
        </span>
      {/if}
    </div>

    <div class="flex h-2 w-full overflow-hidden rounded-full bg-border">
      {#each buckets as b (b.key)}
        <div
          class="h-full {b.color} {b.animate ? 'animate-pulse' : ''} transition-all"
          style="width: {(b.count / total) * 100}%"
          title="{b.label}: {b.count}"
        ></div>
      {/each}
    </div>

    <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs">
      {#each buckets as b (b.key)}
        <span class="flex items-center gap-1.5">
          <span class="h-2.5 w-2.5 rounded-sm {b.color}"></span>
          <span class="text-muted-foreground">{b.label}</span>
          <span class="font-medium tabular-nums">{b.count}</span>
        </span>
      {/each}
    </div>
  </div>
{/if}

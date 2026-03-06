<script lang="ts" generics="T">
  import { X } from 'lucide-svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import DataTableExport from './DataTableExport.svelte';
  import type { DataSource } from './types.js';
  import type { Snippet } from 'svelte';

  let {
    source,
    toolbarLeft,
    toolbarRight,
    exportFilename = 'export.csv',
  }: {
    source: DataSource<T>;
    toolbarLeft?: Snippet;
    toolbarRight?: Snippet;
    exportFilename?: string;
  } = $props();

  const activeFilters = $derived([...source.filters.entries()].filter(([, v]) => v.size > 0));
  const hasContent = $derived(
    !!toolbarLeft || !!toolbarRight || activeFilters.length > 0 || source.exportEnabled,
  );

  function filterLabel(key: string, values: Set<string>): string {
    const col = source.columns.find((c) => c.key === key);
    const name = col?.header ?? key;

    if (col?.type === 'date') {
      const [from, to] = ([...values][0] ?? '|').split('|');
      const parts: string[] = [];
      if (from) parts.push(new Date(from).toLocaleDateString());
      if (to) parts.push(new Date(to).toLocaleDateString());
      return `${name}: ${parts.join(' – ')}`;
    }

    const labels = [...values].map((v) => col?.filterLabels?.[v] ?? v);
    return `${name}: ${labels.join(', ')}`;
  }
</script>

{#if hasContent}
  <div class="flex items-center justify-between gap-2 flex-wrap">
    <div class="flex items-center gap-2 flex-wrap">
      {#if toolbarLeft}
        {@render toolbarLeft()}
      {/if}
      {#each activeFilters as [key, values] (key)}
        <Badge variant="secondary" class="gap-1 font-normal pr-1">
          {filterLabel(key, values)}
          <button
            class="ml-1 rounded-full hover:bg-muted p-0.5 -mr-0.5"
            onclick={() => source.setFilter(key, new Set())}
            aria-label="Clear {key} filter"
          >
            <X class="h-3 w-3" />
          </button>
        </Badge>
      {/each}
      {#if activeFilters.length > 1}
        <button
          class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
          onclick={() => source.clearFilters()}
        >
          Clear all
        </button>
      {/if}
    </div>
    <div class="flex items-center gap-2">
      {#if toolbarRight}
        {@render toolbarRight()}
      {/if}
      {#if source.exportEnabled}
        <DataTableExport {source} filename={exportFilename} />
      {/if}
    </div>
  </div>
{/if}

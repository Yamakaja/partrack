<script lang="ts" generics="T">
  import { X, Columns3 } from 'lucide-svelte';
  import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { cn } from '$lib/utils/cn.js';
  import DataTableExport from './DataTableExport.svelte';
  import type { DataSource } from './types.js';
  import type { Snippet } from 'svelte';

  let {
    source,
    title,
    toolbarLeft,
    toolbarRight,
    showColumnPicker = false,
    exportFilename = 'export.csv',
  }: {
    source: DataSource<T>;
    title?: string;
    toolbarLeft?: Snippet;
    toolbarRight?: Snippet;
    showColumnPicker?: boolean;
    exportFilename?: string;
  } = $props();

  const activeFilters = $derived([...source.filters.entries()].filter(([, v]) => v.size > 0));
  const hasColumnPicker = $derived(showColumnPicker && !!source.setColumnHidden);
  const hasContent = $derived(
    !!title || !!toolbarLeft || !!toolbarRight || activeFilters.length > 0 || source.exportEnabled || hasColumnPicker,
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

    if (col?.type === 'numeric' || col?.type === 'currency') {
      const encoded = [...values][0] ?? '';
      const u = col?.unit ? ` ${col.unit}` : '';
      if (encoded.startsWith('>=')) return `${name}: ≥ ${encoded.slice(2)}${u}`;
      if (encoded.startsWith('<=')) return `${name}: ≤ ${encoded.slice(2)}${u}`;
      if (encoded.startsWith('=')) return `${name}: = ${encoded.slice(1)}${u}`;
      if (encoded.includes('|')) {
        const [lo, hi] = encoded.split('|');
        return `${name}: ${lo}${u} – ${hi}${u}`;
      }
      return `${name}: ${encoded}${u}`;
    }

    const labels = [...values].map((v) => col?.filterLabels?.[v] ?? v);
    if (labels.length > 2) {
      return `${name}: ${labels.slice(0, 2).join(', ')} +${labels.length - 2}`;
    }
    return `${name}: ${labels.join(', ')}`;
  }
</script>

{#if hasContent}
  <div class="flex items-center justify-between gap-2 flex-wrap">
    <div class="flex items-center gap-2 flex-wrap">
      {#if title}
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">{title}</h3>
      {/if}
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
      {#if hasColumnPicker}
        <DropdownMenuPrimitive.Root>
          <DropdownMenuPrimitive.Trigger>
            <Button variant="outline" size="sm" class="gap-1.5">
              <Columns3 class="h-3.5 w-3.5" />
              Columns
            </Button>
          </DropdownMenuPrimitive.Trigger>
          <DropdownMenuPrimitive.Content
            align="end"
            class="z-50 min-w-40 overflow-hidden rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
          >
            {#each source.columns.filter((c) => !c.exportOnly) as col (col.key)}
              <DropdownMenuPrimitive.Item
                class="flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm cursor-pointer hover:bg-accent hover:text-accent-foreground outline-none select-none"
                onSelect={() => source.setColumnHidden?.(col.key, !col.hidden)}
              >
                <span
                  class={cn(
                    'h-3.5 w-3.5 rounded-sm border border-primary flex-shrink-0',
                    !col.hidden && 'bg-primary',
                  )}
                ></span>
                {col.header || '(unnamed)'}
              </DropdownMenuPrimitive.Item>
            {/each}
          </DropdownMenuPrimitive.Content>
        </DropdownMenuPrimitive.Root>
      {/if}
      {#if source.exportEnabled}
        <DataTableExport {source} filename={exportFilename} />
      {/if}
    </div>
  </div>
{/if}

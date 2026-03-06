<script lang="ts" generics="T">
  import { ChevronLeft, ChevronRight } from 'lucide-svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import type { DataSource } from './types.js';

  let { source }: { source: DataSource<T> } = $props();

  const totalPages = $derived(Math.ceil(source.totalRows / source.pageSize));
  const hasPrev = $derived(source.page > 1);
  const hasNext = $derived(source.page < totalPages);
  const rangeStart = $derived((source.page - 1) * source.pageSize + 1);
  const rangeEnd = $derived(Math.min(source.page * source.pageSize, source.totalRows));

  const PAGE_SIZES = [10, 25, 50, 100];
</script>

{#if source.hasPagination && totalPages > 1}
  <div class="flex items-center justify-between text-sm text-muted-foreground">
    <div class="flex items-center gap-2">
      <span>Rows per page</span>
      <select
        value={String(source.pageSize)}
        onchange={(e) => {
          source.setPageSize(Number((e.target as HTMLSelectElement).value));
          source.setPage(1);
        }}
        class="h-7 rounded border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
      >
        {#each PAGE_SIZES as size (size)}
          <option value={String(size)}>{size}</option>
        {/each}
      </select>
    </div>
    <div class="flex items-center gap-3">
      <span>
        {rangeStart}–{rangeEnd} of {source.totalRows}
      </span>
      <div class="flex items-center gap-1">
        <Button
          variant="outline"
          size="icon"
          class="h-7 w-7"
          disabled={!hasPrev}
          onclick={() => source.setPage(source.page - 1)}
        >
          <ChevronLeft class="h-4 w-4" />
        </Button>
        <span class="px-2">Page {source.page} of {totalPages}</span>
        <Button
          variant="outline"
          size="icon"
          class="h-7 w-7"
          disabled={!hasNext}
          onclick={() => source.setPage(source.page + 1)}
        >
          <ChevronRight class="h-4 w-4" />
        </Button>
      </div>
    </div>
  </div>
{/if}

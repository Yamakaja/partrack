<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { ChevronLeft, ChevronRight } from 'lucide-svelte';

  let {
    total,
    limit,
    offset,
    onchange,
  }: {
    total: number;
    limit: number;
    offset: number;
    onchange: (newOffset: number) => void;
  } = $props();

  let currentPage = $derived(Math.floor(offset / limit) + 1);
  let totalPages = $derived(Math.ceil(total / limit));
  let hasPrev = $derived(offset > 0);
  let hasNext = $derived(offset + limit < total);

  function prev() { onchange(Math.max(0, offset - limit)); }
  function next() { onchange(offset + limit); }
</script>

{#if totalPages > 1}
  <div class="flex items-center justify-between text-sm text-muted-foreground">
    <span>
      {offset + 1}–{Math.min(offset + limit, total)} of {total}
    </span>
    <div class="flex items-center gap-1">
      <Button variant="outline" size="icon" disabled={!hasPrev} onclick={prev}>
        <ChevronLeft class="h-4 w-4" />
      </Button>
      <span class="px-2">Page {currentPage} of {totalPages}</span>
      <Button variant="outline" size="icon" disabled={!hasNext} onclick={next}>
        <ChevronRight class="h-4 w-4" />
      </Button>
    </div>
  </div>
{/if}

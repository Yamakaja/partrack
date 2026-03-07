<script lang="ts">
  import { Popover } from 'bits-ui';
  import { Filter } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';

  let {
    columnLabel,
    isFiltered,
    values,
    selectedValues,
    getLabel = (v: string) => v,
    onToggle,
    onClear,
  }: {
    columnLabel: string;
    isFiltered: boolean;
    values: string[];
    selectedValues: Set<string>;
    getLabel?: (v: string) => string;
    onToggle: (value: string) => void;
    onClear: () => void;
  } = $props();

  let open = $state(false);
  let search = $state('');

  function handleOpenChange(v: boolean) {
    if (!v) search = '';
    open = v;
  }

  const filtered = $derived(
    search.trim()
      ? values.filter((v) => getLabel(v).toLowerCase().includes(search.trim().toLowerCase()))
      : values,
  );

  const selectedCount = $derived(selectedValues.size);

  // Drag-select state
  let dragging = $state(false);
  let dragMode = $state(true); // true = selecting, false = deselecting

  function startDrag(event: MouseEvent, value: string) {
    event.preventDefault(); // prevent text selection
    dragMode = !selectedValues.has(value);
    dragging = true;
    onToggle(value);
  }

  function dragOver(value: string) {
    if (!dragging) return;
    const selected = selectedValues.has(value);
    if (dragMode && !selected) onToggle(value);
    if (!dragMode && selected) onToggle(value);
  }

  $effect(() => {
    const stop = () => { dragging = false; };
    window.addEventListener('mouseup', stop);
    return () => window.removeEventListener('mouseup', stop);
  });
</script>

<Popover.Root {open} onOpenChange={handleOpenChange}>
  <Popover.Trigger>
    <button
      class={cn(
        'rounded p-0.5 hover:bg-accent transition-colors',
        isFiltered ? 'text-primary' : 'text-muted-foreground opacity-60 hover:opacity-100',
      )}
      aria-label="Filter {columnLabel}"
    >
      <Filter class="h-3 w-3" />
    </button>
  </Popover.Trigger>

  <Popover.Content
    align="start"
    class="z-50 w-52 rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    <!-- Search -->
    <div class="border-b border-border px-2 py-1.5">
      <input
        type="search"
        bind:value={search}
        placeholder="Search..."
        class="w-full bg-transparent text-xs placeholder:text-muted-foreground focus:outline-none"
      />
    </div>

    <!-- List -->
    <div class="max-h-48 overflow-y-auto py-1 select-none">
      {#if filtered.length === 0}
        <p class="px-2 py-1.5 text-xs text-muted-foreground">No results</p>
      {:else}
        {#each filtered as value (value)}
          <button
            class="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground text-left"
            onmousedown={(e) => startDrag(e, value)}
            onmouseenter={() => dragOver(value)}
          >
            <span
              class={cn(
                'h-3.5 w-3.5 flex-shrink-0 rounded-sm border border-primary transition-colors',
                selectedValues.has(value) && 'bg-primary',
              )}
            ></span>
            {getLabel(value)}
          </button>
        {/each}
      {/if}
    </div>

    <!-- Footer -->
    {#if isFiltered}
      <div class="border-t border-border px-2 py-1.5 flex items-center justify-between">
        <span class="text-xs text-muted-foreground">{selectedCount} selected</span>
        <button
          class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
          onclick={() => { onClear(); open = false; }}
        >
          Clear
        </button>
      </div>
    {/if}
  </Popover.Content>
</Popover.Root>

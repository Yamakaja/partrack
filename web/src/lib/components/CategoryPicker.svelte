<script lang="ts">
  import { Popover } from 'bits-ui';
  import { ChevronDown, ChevronRight, Check } from 'lucide-svelte';
  import type { Category } from '$lib/types/categories.js';
  import { cn } from '$lib/utils/cn.js';

  let {
    value,
    categories,
    onSelect,
    placeholder = 'Select category…',
    disabled = false,
    availableIds = undefined,
  }: {
    value: number | null;
    categories: Category[];
    onSelect: (id: number | null) => void;
    placeholder?: string;
    disabled?: boolean;
    /** When set, restricts the picker to these category IDs (plus their ancestors in tree view). */
    availableIds?: Set<number>;
  } = $props();

  let open = $state(false);
  let search = $state('');

  function handleOpenChange(v: boolean) {
    if (!v) search = '';
    open = v;
  }

  // Map: parent_id → sorted children
  const childrenOf = $derived.by(() => {
    const map = new Map<number | null, Category[]>();
    for (const cat of categories) {
      const key = cat.parent_id;
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(cat);
    }
    // Sort each group by sort_order, then id
    for (const [, children] of map) {
      children.sort((a, b) => a.sort_order - b.sort_order || a.id - b.id);
    }
    return map;
  });

  const categoryById = $derived.by(() => {
    const map = new Map<number, Category>();
    for (const cat of categories) map.set(cat.id, cat);
    return map;
  });

  const selectedCategory = $derived(value != null ? categoryById.get(value) : undefined);

  // When availableIds is set, expand it to include all ancestor IDs so the tree hierarchy is navigable.
  // In search mode, only the original availableIds are shown (no ancestor-only entries).
  const visibleIds = $derived.by(() => {
    if (!availableIds) return null; // null = show all
    const visible = new Set<number>(availableIds);
    for (const id of availableIds) {
      let cat = categoryById.get(id);
      while (cat?.parent_id != null) {
        visible.add(cat.parent_id);
        cat = categoryById.get(cat.parent_id);
      }
    }
    return visible;
  });

  // Expand all root categories by default
  let expanded = $state<Set<number>>(new Set());
  $effect(() => {
    const roots = childrenOf.get(null) ?? [];
    expanded = new Set(roots.map((c) => c.id));
  });

  function toggleExpand(id: number, e: MouseEvent) {
    e.stopPropagation();
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }

  function select(id: number | null) {
    onSelect(id);
    open = false;
  }

  // --- Search mode ---

  const trimmed = $derived(search.trim().toLowerCase());

  function getAncestorName(cat: Category): string {
    const parts: string[] = [cat.display_name];
    let current = cat;
    while (current.parent_id != null) {
      const parent = categoryById.get(current.parent_id);
      if (!parent) break;
      parts.unshift(parent.display_name);
      current = parent;
    }
    return parts.join(' › ');
  }

  const searchResults = $derived.by(() => {
    if (!trimmed) return null;
    return categories
      .filter((c) => c.display_name.toLowerCase().includes(trimmed) && (!visibleIds || visibleIds.has(c.id)))
      .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id);
  });
</script>

<Popover.Root {open} onOpenChange={handleOpenChange}>
  <Popover.Trigger {disabled} class="w-full">
    <button
      type="button"
      {disabled}
      class={cn(
        'flex h-9 w-full items-center justify-between rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm',
        'hover:bg-accent/50 transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        'disabled:opacity-50 disabled:pointer-events-none',
        !selectedCategory && 'text-muted-foreground',
      )}
    >
      <span class="truncate">{selectedCategory?.display_name ?? placeholder}</span>
      <ChevronDown class="h-4 w-4 flex-shrink-0 text-muted-foreground ml-2" />
    </button>
  </Popover.Trigger>

  <Popover.Content
    align="start"
    sideOffset={4}
    class="z-50 min-w-[480px] w-max max-w-[640px] rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    <!-- Search -->
    <div class="border-b border-border px-2 py-1.5">
      <input
        type="search"
        bind:value={search}
        placeholder="Search…"
        class="w-full bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
      />
    </div>

    <!-- List -->
    <div class="max-h-64 overflow-y-auto py-1">
      <!-- None option -->
      <button
        type="button"
        class={cn(
          'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left',
          value == null && 'bg-accent/60',
        )}
        onclick={() => select(null)}
      >
        <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
          {#if value == null}
            <Check class="h-3.5 w-3.5" />
          {/if}
        </span>
        <span class="text-muted-foreground italic">None</span>
      </button>

      {#if searchResults}
        <!-- Flat filtered results -->
        {#if searchResults.length === 0}
          <p class="px-4 py-2 text-sm text-muted-foreground">No results</p>
        {:else}
          {#each searchResults as cat (cat.id)}
            <button
              type="button"
              class={cn(
                'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left',
                value === cat.id && 'bg-accent font-medium',
              )}
              onclick={() => select(cat.id)}
            >
              <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
                {#if value === cat.id}
                  <Check class="h-3.5 w-3.5" />
                {/if}
              </span>
              <span>{getAncestorName(cat)}</span>
            </button>
          {/each}
        {/if}
      {:else}
        <!-- Hierarchical tree -->
        {#each childrenOf.get(null) ?? [] as root (root.id)}
          {#if !visibleIds || visibleIds.has(root.id)}
          {@const rootChildren = (childrenOf.get(root.id) ?? []).filter((c) => !visibleIds || visibleIds.has(c.id))}
          {@const isExpanded = expanded.has(root.id)}

          <!-- Root row -->
          <div class="flex w-full items-center">
            <button
              type="button"
              class={cn(
                'flex flex-1 items-center gap-1 rounded-sm px-2 py-1.5 text-sm hover:bg-accent text-left min-w-0',
                value === root.id && 'bg-accent font-medium',
              )}
              onclick={() => select(root.id)}
            >
              <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
                {#if value === root.id}
                  <Check class="h-3.5 w-3.5" />
                {/if}
              </span>
              <span>{root.display_name}</span>
            </button>
            {#if rootChildren.length > 0}
              <button
                type="button"
                class="px-1.5 py-1.5 text-muted-foreground hover:text-foreground flex-shrink-0"
                onclick={(e) => toggleExpand(root.id, e)}
                aria-label={isExpanded ? 'Collapse' : 'Expand'}
              >
                {#if isExpanded}
                  <ChevronDown class="h-3.5 w-3.5" />
                {:else}
                  <ChevronRight class="h-3.5 w-3.5" />
                {/if}
              </button>
            {/if}
          </div>

          <!-- Children -->
          {#if isExpanded}
            {#each rootChildren as child (child.id)}
              {@const grandchildren = (childrenOf.get(child.id) ?? []).filter((c) => !visibleIds || visibleIds.has(c.id))}
              {@const childExpanded = expanded.has(child.id)}
              <div class="flex w-full items-center">
                <button
                  type="button"
                  class={cn(
                    'flex flex-1 items-center gap-2 rounded-sm pl-7 pr-2 py-1.5 text-sm hover:bg-accent text-left min-w-0',
                    value === child.id && 'bg-accent font-medium',
                  )}
                  onclick={() => select(child.id)}
                >
                  <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
                    {#if value === child.id}
                      <Check class="h-3.5 w-3.5" />
                    {/if}
                  </span>
                  <span>{child.display_name}</span>
                </button>
                {#if grandchildren.length > 0}
                  <button
                    type="button"
                    class="px-1.5 py-1.5 text-muted-foreground hover:text-foreground flex-shrink-0"
                    onclick={(e) => toggleExpand(child.id, e)}
                    aria-label={childExpanded ? 'Collapse' : 'Expand'}
                  >
                    {#if childExpanded}
                      <ChevronDown class="h-3.5 w-3.5" />
                    {:else}
                      <ChevronRight class="h-3.5 w-3.5" />
                    {/if}
                  </button>
                {/if}
              </div>
              <!-- Grandchildren -->
              {#if childExpanded}
                {#each grandchildren as grandchild (grandchild.id)}
                  <button
                    type="button"
                    class={cn(
                      'flex w-full items-center gap-2 rounded-sm pl-14 pr-2 py-1.5 text-sm hover:bg-accent text-left',
                      value === grandchild.id && 'bg-accent font-medium',
                    )}
                    onclick={() => select(grandchild.id)}
                  >
                    <span class="h-4 w-4 flex-shrink-0 flex items-center justify-center">
                      {#if value === grandchild.id}
                        <Check class="h-3.5 w-3.5" />
                      {/if}
                    </span>
                    <span>{grandchild.display_name}</span>
                  </button>
                {/each}
              {/if}
            {/each}
          {/if}
          {/if}
        {/each}
      {/if}
    </div>
  </Popover.Content>
</Popover.Root>

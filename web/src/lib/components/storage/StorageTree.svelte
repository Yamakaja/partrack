<script lang="ts">
  import { ChevronDown, ChevronRight } from 'lucide-svelte';
  import type { Snippet } from 'svelte';
  import type { StorageLocation } from '$lib/types/inventory.js';
  import { cn } from '$lib/utils/cn.js';

  interface Props {
    locations: StorageLocation[];
    selectedId?: string | null;
    onSelect: (loc: StorageLocation) => void;
    /** Optional leading icon/indicator inside the label button. Receives (loc, isSelected). */
    rowLeading?: Snippet<[StorageLocation, boolean]>;
    /** Optional trailing actions revealed on row hover. Receives (loc). */
    rowActions?: Snippet<[StorageLocation]>;
  }

  let { locations, selectedId = null, onSelect, rowLeading, rowActions }: Props = $props();

  // ── Tree helpers ────────────────────────────────────────────────────────────

  const locById = $derived.by(() => {
    const m = new Map<string, StorageLocation>();
    for (const l of locations) m.set(l.id, l);
    return m;
  });

  const childrenOf = $derived.by(() => {
    const m = new Map<string | null, StorageLocation[]>();
    for (const l of locations) {
      const key = l.parent_id;
      if (!m.has(key)) m.set(key, []);
      m.get(key)!.push(l);
    }
    for (const [, children] of m) {
      children.sort((a, b) => a.label.localeCompare(b.label));
    }
    return m;
  });

  // ── Expand/collapse ─────────────────────────────────────────────────────────

  let expanded = $state<Set<string>>(new Set());

  $effect(() => {
    // Always expand root nodes; also expand all ancestors of the selected item
    // so it's visible in the tree without manual navigation.
    const roots = childrenOf.get(null) ?? [];
    const next = new Set(roots.map((l) => l.id));
    if (selectedId) {
      let cur = locById.get(selectedId);
      while (cur?.parent_id) {
        next.add(cur.parent_id);
        cur = locById.get(cur.parent_id);
      }
    }
    expanded = next;
  });

  function toggleExpand(id: string, e: MouseEvent) {
    e.stopPropagation();
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }

  // ── Flat tree ───────────────────────────────────────────────────────────────

  interface FlatNode { loc: StorageLocation; depth: number }

  const flatTree = $derived.by((): FlatNode[] => {
    const result: FlatNode[] = [];
    function visit(parentId: string | null, depth: number) {
      const children = childrenOf.get(parentId) ?? [];
      for (const loc of children) {
        result.push({ loc, depth });
        if (expanded.has(loc.id)) visit(loc.id, depth + 1);
      }
    }
    visit(null, 0);
    return result;
  });
</script>

{#each flatTree as { loc, depth } (loc.id)}
  {@const children = childrenOf.get(loc.id) ?? []}
  {@const hasChildren = children.length > 0 && !loc.grid_config}
  {@const isExpanded = expanded.has(loc.id)}
  {@const isSelected = selectedId === loc.id}

  <div class={cn('group flex w-full items-center hover:bg-accent/60', isSelected && 'bg-accent')}>

    <!-- Chevron (left) or indent spacer -->
    {#if hasChildren}
      <button
        type="button"
        style="padding-left: {0.25 + depth * 1.125}rem"
        class="flex-shrink-0 p-1.5 text-muted-foreground hover:text-foreground transition-colors"
        onclick={(e) => toggleExpand(loc.id, e)}
        aria-label={isExpanded ? 'Collapse' : 'Expand'}
      >
        {#if isExpanded}
          <ChevronDown class="h-3.5 w-3.5" />
        {:else}
          <ChevronRight class="h-3.5 w-3.5" />
        {/if}
      </button>
    {:else}
      <!-- Spacer: aligns with p-1.5 padding on each side (0.375rem × 2) + w-3.5 icon (0.875rem) = 1.625rem total -->
      <span style="padding-left: {0.25 + depth * 1.125 + 1.625}rem" class="flex-shrink-0"></span>
    {/if}

    <!-- Label / select button -->
    <button
      type="button"
      class="flex flex-1 items-center gap-1.5 py-1.5 pr-1 text-sm text-left min-w-0"
      onclick={() => onSelect(loc)}
    >
      {#if rowLeading}
        {@render rowLeading(loc, isSelected)}
      {/if}
      <span class="truncate">{loc.label}</span>
    </button>

    <!-- Trailing hover actions (optional) -->
    {#if rowActions}
      <div class="flex items-center gap-0.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity pr-1">
        {@render rowActions(loc)}
      </div>
    {/if}

  </div>
{/each}

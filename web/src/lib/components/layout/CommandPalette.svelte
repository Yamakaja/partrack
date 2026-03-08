<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Cpu, FileText, ShoppingCart, Wrench, Package, Clock, Search } from 'lucide-svelte';
  import { navigate } from '$lib/router/index.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { paletteStore } from '$lib/stores/palette.svelte.js';
  import { globalSearch, type SearchResults } from '$lib/api/search.js';

  let teamId = $derived(activeTeamStore.current?.teamId ?? '');

  // ── State ────────────────────────────────────────────────────────────────

  let query = $state('');
  let results = $state<SearchResults | null>(null);
  let loading = $state(false);
  let selectedIndex = $state(0);
  let inputEl = $state<HTMLInputElement | null>(null);

  // ── Recent items (localStorage) ──────────────────────────────────────────

  interface RecentItem {
    type: 'part' | 'project' | 'order' | 'build' | 'storage';
    id: string;
    primary: string;
    secondary: string;
    url: string;
  }

  function recentKey() {
    return `partrack-recent-${teamId}`;
  }

  function loadRecent(): RecentItem[] {
    if (!teamId) return [];
    try {
      return JSON.parse(localStorage.getItem(recentKey()) ?? '[]');
    } catch {
      return [];
    }
  }

  function saveRecent(item: RecentItem) {
    const key = recentKey();
    let items = loadRecent();
    items = items.filter(i => !(i.type === item.type && i.id === item.id));
    items.unshift(item);
    if (items.length > 20) items = items.slice(0, 20);
    localStorage.setItem(key, JSON.stringify(items));
  }

  // ── Flat result list for keyboard navigation ──────────────────────────────

  interface ResultItem {
    type: 'part' | 'project' | 'order' | 'build' | 'storage';
    id: string;
    primary: string;
    secondary: string;
    badge?: string;
    url: string;
  }

  let flatItems = $derived.by(() => {
    if (!query.trim() && !results) {
      return loadRecent().slice(0, 5).map(r => ({ ...r, badge: undefined }));
    }
    if (!results) return [] as ResultItem[];
    const items: ResultItem[] = [];

    for (const p of results.parts) {
      items.push({
        type: 'part',
        id: p.id,
        primary: p.mpn ?? '(no MPN)',
        secondary: [p.manufacturer, p.description].filter(Boolean).join(' · '),
        badge: p.lifecycle_status !== 'active' ? p.lifecycle_status : undefined,
        url: `/teams/${teamId}/parts/${p.id}`,
      });
    }
    for (const p of results.projects) {
      items.push({
        type: 'project',
        id: p.id,
        primary: p.name,
        secondary: p.status,
        badge: p.status !== 'active' ? p.status : undefined,
        url: `/teams/${teamId}/projects/${p.id}`,
      });
    }
    for (const o of results.orders) {
      const label = o.purchase_order_number ?? o.external_reference ?? o.id;
      items.push({
        type: 'order',
        id: o.id,
        primary: `#${label}`,
        secondary: o.distributor_name,
        badge: o.status,
        url: `/teams/${teamId}/orders/${o.id}`,
      });
    }
    for (const b of results.builds) {
      items.push({
        type: 'build',
        id: b.id,
        primary: `${b.project_name} ${b.revision_label} ×${b.quantity}`,
        secondary: b.status,
        badge: b.status !== 'assembled' ? b.status : undefined,
        url: `/teams/${teamId}/builds/${b.id}`,
      });
    }
    for (const s of results.storage_locations) {
      items.push({
        type: 'storage',
        id: s.id,
        primary: s.label,
        secondary: s.description ?? '',
        url: `/teams/${teamId}/storage`,
      });
    }
    return items;
  });

  // ── Grouped sections for rendering ───────────────────────────────────────

  interface Section {
    label: string;
    items: ResultItem[];
    startIndex: number;
  }

  let sections = $derived.by((): Section[] => {
    if (!query.trim() && !results) {
      const recent = loadRecent().slice(0, 5).map(r => ({ ...r, badge: undefined }));
      return recent.length > 0 ? [{ label: 'Recent', items: recent, startIndex: 0 }] : [];
    }
    if (!results) return [];
    const groups: { label: string; type: ResultItem['type'] }[] = [
      { label: 'Parts', type: 'part' },
      { label: 'Projects', type: 'project' },
      { label: 'Orders', type: 'order' },
      { label: 'Builds', type: 'build' },
      { label: 'Storage', type: 'storage' },
    ];
    let idx = 0;
    return groups
      .map(g => {
        const items = flatItems.filter(i => i.type === g.type);
        const s: Section = { label: g.label, items, startIndex: idx };
        idx += items.length;
        return s;
      })
      .filter(s => s.items.length > 0);
  });

  // ── Debounced search ──────────────────────────────────────────────────────

  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  $effect(() => {
    const q = query;
    if (debounceTimer) clearTimeout(debounceTimer);
    if (q.trim().length < 2) {
      results = null;
      loading = false;
      return;
    }
    loading = true;
    debounceTimer = setTimeout(async () => {
      if (!teamId) return;
      try {
        results = await globalSearch(teamId, q.trim());
      } catch {
        results = null;
      } finally {
        loading = false;
      }
    }, 200);
  });

  // ── Reset state on open ───────────────────────────────────────────────────

  $effect(() => {
    if (paletteStore.open) {
      query = '';
      results = null;
      selectedIndex = 0;
      setTimeout(() => inputEl?.focus(), 0);
    }
  });

  // Clamp selectedIndex when flat list changes
  $effect(() => {
    if (selectedIndex >= flatItems.length) {
      selectedIndex = Math.max(0, flatItems.length - 1);
    }
  });

  // ── Actions ───────────────────────────────────────────────────────────────

  function go(item: ResultItem) {
    saveRecent({ type: item.type, id: item.id, primary: item.primary, secondary: item.secondary, url: item.url });
    paletteStore.open = false;
    navigate(item.url);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selectedIndex = Math.min(selectedIndex + 1, flatItems.length - 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selectedIndex = Math.max(selectedIndex - 1, 0);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (flatItems[selectedIndex]) go(flatItems[selectedIndex]);
    }
  }

  // ── Icon helper ───────────────────────────────────────────────────────────

  const icons: Record<ResultItem['type'], any> = {
    part: Cpu,
    project: FileText,
    order: ShoppingCart,
    build: Wrench,
    storage: Package,
  };
</script>

<Dialog.Root
  open={paletteStore.open}
  onOpenChange={(v) => { paletteStore.open = v; }}
>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/60" />
    <Dialog.Content
      class="fixed left-1/2 top-[20%] z-50 w-full max-w-xl -translate-x-1/2 rounded-xl border border-border bg-background shadow-2xl overflow-hidden"
      onkeydown={onKeydown}
    >
      <!-- Search input -->
      <div class="flex items-center gap-2 px-4 py-3 border-b border-border">
        <Search class="size-4 shrink-0 text-muted-foreground" />
        <input
          bind:this={inputEl}
          bind:value={query}
          type="text"
          placeholder="Search parts, projects, orders..."
          class="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        />
        {#if loading}
          <span class="text-xs text-muted-foreground">Searching…</span>
        {/if}
        <kbd class="hidden sm:inline-flex items-center rounded border border-border px-1.5 py-0.5 text-xs text-muted-foreground font-mono">Esc</kbd>
      </div>

      <!-- Results -->
      <div class="max-h-96 overflow-y-auto">
        {#if sections.length === 0 && query.trim().length >= 2 && !loading}
          <div class="px-4 py-8 text-center text-sm text-muted-foreground">No results for "{query}"</div>
        {:else if sections.length === 0 && query.trim().length === 0}
          <div class="px-4 py-8 text-center text-sm text-muted-foreground">
            Start typing to search, or use arrow keys to navigate recent items.
          </div>
        {:else}
          {#each sections as section}
            <div>
              <div class="px-4 pt-3 pb-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1">
                {#if section.label === 'Recent'}
                  <Clock class="size-3" />
                {/if}
                {section.label}
              </div>
              {#each section.items as item, i}
                {@const globalIdx = section.startIndex + i}
                {@const Icon = icons[item.type]}
                <button
                  type="button"
                  class="w-full flex items-center gap-3 px-4 py-2 text-left transition-colors
                    {globalIdx === selectedIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50'}"
                  onclick={() => go(item)}
                  onmouseenter={() => { selectedIndex = globalIdx; }}
                >
                  <Icon class="size-4 shrink-0 text-muted-foreground" />
                  <div class="flex-1 min-w-0">
                    <div class="text-sm font-medium truncate">{item.primary}</div>
                    {#if item.secondary}
                      <div class="text-xs text-muted-foreground truncate">{item.secondary}</div>
                    {/if}
                  </div>
                  {#if item.badge}
                    <span class="shrink-0 rounded px-1.5 py-0.5 text-xs font-medium bg-muted text-muted-foreground capitalize">{item.badge}</span>
                  {/if}
                </button>
              {/each}
            </div>
          {/each}
        {/if}
      </div>

      <!-- Footer hint -->
      <div class="border-t border-border px-4 py-2 flex items-center gap-3 text-xs text-muted-foreground">
        <span><kbd class="font-mono">↑↓</kbd> navigate</span>
        <span><kbd class="font-mono">↵</kbd> open</span>
        <span><kbd class="font-mono">Esc</kbd> close</span>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

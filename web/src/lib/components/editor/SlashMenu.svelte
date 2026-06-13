<script lang="ts">
  import { globalSearch, type SearchResults } from '$lib/api/search.js';
  import { KIND_META, type ObjectKind } from '$lib/components/notes/objectKinds.js';
  import type { SlashActions } from './types.js';
  import { Link2, Image as ImageIcon } from 'lucide-svelte';

  let {
    teamId,
    query = '',
    actions,
  }: { teamId: string; query?: string; actions: SlashActions } = $props();

  type Mode = 'commands' | 'objects';
  let mode = $state<Mode>('commands');
  let objQuery = $state('');
  let results = $state<SearchResults | null>(null);
  let loading = $state(false);
  let searchInput = $state<HTMLInputElement>();
  let fileInput = $state<HTMLInputElement>();
  let timer: ReturnType<typeof setTimeout>;

  const commands = [
    { id: 'object', label: 'Link object', hint: 'part, project, order…', icon: Link2 },
    { id: 'image', label: 'Insert image', hint: 'Upload an image', icon: ImageIcon },
  ];

  let filtered = $derived(
    commands.filter((c) => c.label.toLowerCase().includes(query.toLowerCase())),
  );

  interface PickItem {
    kind: ObjectKind;
    id: string;
    label: string;
    sub?: string;
  }

  // Flattens the heterogeneous search payload into kind-grouped sections, each
  // with a display label (the fallback stored in the body) and a subtitle.
  let sections = $derived.by((): { kind: ObjectKind; title: string; items: PickItem[] }[] => {
    const r = results;
    if (!r) return [];
    const out = [
      {
        kind: 'part' as const,
        title: KIND_META.part.label,
        items: r.parts.map((p) => ({ kind: 'part' as const, id: p.id, label: p.mpn ?? '—', sub: p.manufacturer ?? undefined })),
      },
      {
        kind: 'project' as const,
        title: KIND_META.project.label,
        items: r.projects.map((p) => ({ kind: 'project' as const, id: p.id, label: p.name, sub: p.status })),
      },
      {
        kind: 'revision' as const,
        title: KIND_META.revision.label,
        items: r.revisions.map((rv) => ({ kind: 'revision' as const, id: rv.id, label: `${rv.project_name} ${rv.label}`, sub: 'revision' })),
      },
      {
        kind: 'order' as const,
        title: KIND_META.order.label,
        items: r.orders.map((o) => ({ kind: 'order' as const, id: o.id, label: o.purchase_order_number ?? o.external_reference ?? o.distributor_name, sub: o.distributor_name })),
      },
      {
        kind: 'build' as const,
        title: KIND_META.build.label,
        items: r.builds.map((b) => ({ kind: 'build' as const, id: b.id, label: `${b.project_name} ${b.revision_label}`, sub: b.status })),
      },
      {
        kind: 'storage' as const,
        title: KIND_META.storage.label,
        items: r.storage_locations.map((s) => ({ kind: 'storage' as const, id: s.id, label: s.label, sub: s.ancestor_path ?? undefined })),
      },
    ];
    return out.filter((s) => s.items.length > 0);
  });

  let hasResults = $derived(sections.some((s) => s.items.length > 0));

  // Called by the slash provider each time the menu is (re)shown.
  export function reset() {
    mode = 'commands';
    objQuery = '';
    results = null;
  }

  function runCommand(id: string) {
    if (id === 'object') {
      mode = 'objects';
      setTimeout(() => searchInput?.focus(), 0);
    } else if (id === 'image') {
      fileInput?.click();
    }
  }

  async function search(q: string) {
    if (q.length < 2) {
      results = null;
      return;
    }
    loading = true;
    try {
      results = await globalSearch(teamId, q);
    } catch {
      results = null;
    } finally {
      loading = false;
    }
  }

  function onObjInput(e: Event) {
    objQuery = (e.target as HTMLInputElement).value;
    clearTimeout(timer);
    timer = setTimeout(() => search(objQuery), 250);
  }

  function onFile(e: Event) {
    const f = (e.target as HTMLInputElement).files?.[0];
    if (f) actions.insertImageFile(f);
    (e.target as HTMLInputElement).value = '';
  }
</script>

<div class="w-72 overflow-hidden rounded-md border border-border bg-popover text-popover-foreground shadow-md">
  <input bind:this={fileInput} type="file" accept="image/*" class="hidden" onchange={onFile} />

  {#if mode === 'commands'}
    {#if filtered.length === 0}
      <div class="px-3 py-2 text-sm text-muted-foreground">No matches</div>
    {/if}
    {#each filtered as cmd (cmd.id)}
      <button
        type="button"
        class="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent hover:text-accent-foreground"
        onmousedown={(e) => {
          e.preventDefault();
          runCommand(cmd.id);
        }}
      >
        <cmd.icon class="h-4 w-4 shrink-0 text-muted-foreground" />
        <span class="flex-1">{cmd.label}</span>
        <span class="text-xs text-muted-foreground">{cmd.hint}</span>
      </button>
    {/each}
  {:else}
    <div class="border-b border-border p-2">
      <input
        bind:this={searchInput}
        value={objQuery}
        oninput={onObjInput}
        onmousedown={(e) => e.stopPropagation()}
        placeholder="Search parts, projects, orders…"
        class="h-8 w-full rounded border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
    </div>
    <div class="max-h-64 overflow-y-auto">
      {#if loading}
        <div class="px-3 py-2 text-sm text-muted-foreground">Searching…</div>
      {:else if !hasResults}
        <div class="px-3 py-2 text-sm text-muted-foreground">
          {objQuery.length < 2 ? 'Type to search' : 'No matches'}
        </div>
      {:else}
        {#each sections as section (section.kind)}
          {@const SectionIcon = KIND_META[section.kind].icon}
          <div class="px-3 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {section.title}
          </div>
          {#each section.items as item (item.id)}
            <button
              type="button"
              class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
              onmousedown={(e) => {
                e.preventDefault();
                actions.insertObject(item.kind, item.id, item.label);
              }}
            >
              <SectionIcon class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span class="truncate font-medium">{item.label}</span>
              {#if item.sub}
                <span class="ml-auto shrink-0 truncate text-xs text-muted-foreground">{item.sub}</span>
              {/if}
            </button>
          {/each}
        {/each}
      {/if}
    </div>
  {/if}
</div>

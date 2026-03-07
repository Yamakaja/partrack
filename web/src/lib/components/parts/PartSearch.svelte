<script lang="ts">
  import { listParts } from '$lib/api/parts.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import { Search } from 'lucide-svelte';

  let {
    teamId,
    value = $bindable<PartSummary | null>(null),
    placeholder = 'Search parts by MPN…',
    initialQuery = '',
    onselect,
  }: {
    teamId?: string;
    value?: PartSummary | null;
    placeholder?: string;
    initialQuery?: string;
    onselect?: (part: PartSummary) => void;
  } = $props();

  let query = $state('');
  let results = $state<PartSummary[]>([]);
  let loading = $state(false);
  let open = $state(false);
  let timer: ReturnType<typeof setTimeout>;

  $effect(() => {
    if (initialQuery) {
      query = initialQuery;
      search(initialQuery);
    }
  });

  async function search(q: string) {
    if (!q || q.length < 2) { results = []; return; }
    loading = true;
    try {
      const r = await listParts({ team_id: teamId, q, limit: 10, offset: 0 });
      results = r.items;
      open = results.length > 0;
    } catch { results = []; }
    finally { loading = false; }
  }

  function handleInput(e: Event) {
    query = (e.target as HTMLInputElement).value;
    clearTimeout(timer);
    timer = setTimeout(() => search(query), 300);
    open = false;
  }

  function select(part: PartSummary) {
    value = part;
    query = part.mpn ?? '';
    open = false;
    onselect?.(part);
  }
</script>

<div class="relative">
  <div class="relative">
    <Search class="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
    <input
      type="text"
      value={value ? (value.mpn ?? '') : query}
      oninput={handleInput}
      onfocus={() => { if (results.length) open = true; }}
      onblur={() => setTimeout(() => (open = false), 150)}
      {placeholder}
      class="flex h-9 w-full rounded-md border border-input bg-background pl-8 pr-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    />
  </div>
  {#if open && results.length > 0}
    <ul class="absolute z-50 mt-1 w-full rounded-md border border-border bg-popover shadow-md max-h-60 overflow-y-auto">
      {#each results as part (part.id)}
        <li>
          <button
            type="button"
            class="w-full px-3 py-2 text-left text-sm hover:bg-accent hover:text-accent-foreground"
            onmousedown={() => select(part)}
          >
            <span class="font-medium">{part.mpn ?? '—'}</span>
            {#if part.manufacturer}
              <span class="ml-1 text-muted-foreground">{part.manufacturer}</span>
            {/if}
            {#if part.short_description ?? part.description}
              <span class="block text-xs text-muted-foreground truncate">{part.short_description ?? part.description}</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

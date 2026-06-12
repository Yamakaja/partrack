<script lang="ts">
  import { listParts } from '$lib/api/parts.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import type { SlashActions } from './types.js';
  import { Cpu, Image as ImageIcon } from 'lucide-svelte';

  let {
    teamId,
    query = '',
    actions,
  }: { teamId: string; query?: string; actions: SlashActions } = $props();

  type Mode = 'commands' | 'parts';
  let mode = $state<Mode>('commands');
  let partQuery = $state('');
  let results = $state<PartSummary[]>([]);
  let loading = $state(false);
  let searchInput = $state<HTMLInputElement>();
  let fileInput = $state<HTMLInputElement>();
  let timer: ReturnType<typeof setTimeout>;

  const commands = [
    { id: 'part', label: 'Link part', hint: 'Reference a part', icon: Cpu },
    { id: 'image', label: 'Insert image', hint: 'Upload an image', icon: ImageIcon },
  ];

  let filtered = $derived(
    commands.filter((c) => c.label.toLowerCase().includes(query.toLowerCase())),
  );

  // Called by the slash provider each time the menu is (re)shown.
  export function reset() {
    mode = 'commands';
    partQuery = '';
    results = [];
  }

  function runCommand(id: string) {
    if (id === 'part') {
      mode = 'parts';
      setTimeout(() => searchInput?.focus(), 0);
    } else if (id === 'image') {
      fileInput?.click();
    }
  }

  async function search(q: string) {
    if (q.length < 2) {
      results = [];
      return;
    }
    loading = true;
    try {
      const r = await listParts({ team_id: teamId, q, limit: 8, offset: 0 });
      results = r.items;
    } catch {
      results = [];
    } finally {
      loading = false;
    }
  }

  function onPartInput(e: Event) {
    partQuery = (e.target as HTMLInputElement).value;
    clearTimeout(timer);
    timer = setTimeout(() => search(partQuery), 250);
  }

  function onFile(e: Event) {
    const f = (e.target as HTMLInputElement).files?.[0];
    if (f) actions.insertImageFile(f);
    (e.target as HTMLInputElement).value = '';
  }
</script>

<div class="w-64 overflow-hidden rounded-md border border-border bg-popover text-popover-foreground shadow-md">
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
        value={partQuery}
        oninput={onPartInput}
        onmousedown={(e) => e.stopPropagation()}
        placeholder="Search parts by MPN…"
        class="h-8 w-full rounded border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
    </div>
    <div class="max-h-56 overflow-y-auto">
      {#if loading}
        <div class="px-3 py-2 text-sm text-muted-foreground">Searching…</div>
      {:else if results.length === 0}
        <div class="px-3 py-2 text-sm text-muted-foreground">
          {partQuery.length < 2 ? 'Type to search' : 'No parts found'}
        </div>
      {/if}
      {#each results as part (part.id)}
        <button
          type="button"
          class="block w-full px-3 py-2 text-left text-sm hover:bg-accent hover:text-accent-foreground"
          onmousedown={(e) => {
            e.preventDefault();
            actions.insertPart(part);
          }}
        >
          <span class="font-medium">{part.mpn ?? '—'}</span>
          {#if part.manufacturer}
            <span class="ml-1 text-muted-foreground">{part.manufacturer}</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

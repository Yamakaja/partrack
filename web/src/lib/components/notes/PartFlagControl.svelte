<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getPartNote, setPartFlag, type Note, type PartFlag } from '$lib/api/notes.js';
  import { FLAG_OPTIONS } from './flags.js';
  import { DropdownMenu, DropdownMenuItem } from '$lib/components/ui/dropdown-menu/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import FlagBadge from './FlagBadge.svelte';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ChevronDown, Check, Flag } from 'lucide-svelte';

  let { teamId, partId, canEdit = false }: { teamId: string; partId: string; canEdit?: boolean } = $props();

  const qc = useQueryClient();

  // Shares the note query key, so it dedupes with NotePanel's fetch.
  let noteQuery = createQuery(() => ({
    queryKey: ['part-note', teamId, partId],
    queryFn: () => getPartNote(teamId, partId),
  }));
  let flag = $derived<PartFlag>(noteQuery.data?.flag ?? 'none');
  let open = $state(false);
  let saving = $state(false);

  async function choose(f: PartFlag) {
    open = false;
    if (f === flag) return;
    saving = true;
    try {
      await setPartFlag(teamId, partId, f);
      qc.setQueryData<Note>(['part-note', teamId, partId], (prev) => (prev ? { ...prev, flag: f } : prev));
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : 'Failed to set flag');
    } finally {
      saving = false;
    }
  }
</script>

{#if canEdit}
  <DropdownMenu bind:open align="end">
    {#snippet trigger()}
      <Button variant="outline" size="sm" class="gap-1.5" disabled={saving}>
        {#if flag === 'none'}
          <Flag class="h-3.5 w-3.5 text-muted-foreground" /> Flag
        {:else}
          <FlagBadge {flag} />
        {/if}
        <ChevronDown class="h-3.5 w-3.5 text-muted-foreground" />
      </Button>
    {/snippet}
    {#each FLAG_OPTIONS as f (f)}
      <DropdownMenuItem onSelect={() => choose(f)}>
        {#if f === 'none'}
          <span class="text-sm text-muted-foreground">No flag</span>
        {:else}
          <FlagBadge flag={f} />
        {/if}
        {#if f === flag}<Check class="ml-auto h-3.5 w-3.5" />{/if}
      </DropdownMenuItem>
    {/each}
  </DropdownMenu>
{:else if flag !== 'none'}
  <FlagBadge {flag} />
{/if}

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import type { Component } from 'svelte';
  import { getPartNote, putPartNote } from '$lib/api/notes.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Pencil } from 'lucide-svelte';
  import NoteViewer from './NoteViewer.svelte';

  let { teamId, partId, canEdit = false }: { teamId: string; partId: string; canEdit?: boolean } = $props();

  const qc = useQueryClient();

  let noteQuery = createQuery(() => ({
    queryKey: ['part-note', teamId, partId],
    queryFn: () => getPartNote(teamId, partId),
  }));

  let editing = $state(false);
  let draft = $state('');
  let saving = $state(false);
  // The Milkdown editor is heavy (ProseMirror); load it only when editing so it
  // never hits viewers or the initial part-detail render.
  let EditorCmp = $state<Component<{ teamId: string; value?: string; onChange?: (md: string) => void }> | null>(null);

  let hasBody = $derived((noteQuery.data?.body ?? '').trim().length > 0);

  async function startEdit() {
    draft = noteQuery.data?.body ?? '';
    editing = true;
    if (!EditorCmp) {
      EditorCmp = (await import('$lib/components/editor/MilkdownEditor.svelte')).default;
    }
  }

  function cancel() {
    editing = false;
  }

  async function save() {
    saving = true;
    try {
      const res = await putPartNote(teamId, partId, draft);
      qc.setQueryData(['part-note', teamId, partId], res);
      editing = false;
      pushToast('success', 'Note saved');
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : 'Failed to save note');
    } finally {
      saving = false;
    }
  }
</script>

<section class="space-y-2">
  <div class="flex items-center justify-between">
    <h3 class="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Notes</h3>
    {#if canEdit && !editing}
      <Button variant="ghost" size="sm" onclick={startEdit}>
        <Pencil class="mr-1 h-3.5 w-3.5" />
        {hasBody ? 'Edit' : 'Add note'}
      </Button>
    {/if}
  </div>

  {#if noteQuery.isLoading}
    <Skeleton class="h-24 w-full" />
  {:else if editing}
    <div class="rounded-md border border-border bg-card">
      {#if EditorCmp}
        {@const Editor = EditorCmp}
        <Editor {teamId} value={draft} onChange={(md) => (draft = md)} />
      {:else}
        <div class="p-4 text-sm text-muted-foreground">Loading editor…</div>
      {/if}
    </div>
    <div class="flex justify-end gap-2">
      <Button variant="ghost" size="sm" onclick={cancel} disabled={saving}>Cancel</Button>
      <Button size="sm" onclick={save} disabled={saving || !EditorCmp}>
        {saving ? 'Saving…' : 'Save'}
      </Button>
    </div>
  {:else if hasBody && noteQuery.data}
    <NoteViewer body={noteQuery.data.body} manifest={noteQuery.data.manifest} {teamId} />
  {:else}
    <p class="text-sm text-muted-foreground italic">
      No notes yet.{#if !canEdit} Members can add notes about this part.{/if}
    </p>
  {/if}
</section>

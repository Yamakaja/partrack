<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    listDocuments,
    uploadDocument,
    deleteDocument,
    formatBytes,
    type DocumentItem,
    type DocumentOwnerKind,
  } from '$lib/api/documents.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import DocumentViewerDialog from './DocumentViewerDialog.svelte';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import {
    Upload,
    Eye,
    Download,
    Trash2,
    FileText,
    FileSpreadsheet,
    FileArchive,
    Image as ImageIcon,
    Presentation,
    File as FileIcon,
  } from 'lucide-svelte';

  let {
    teamId,
    ownerKind,
    ownerId,
    canEdit = false,
  }: {
    teamId: string;
    ownerKind: DocumentOwnerKind;
    ownerId: string;
    canEdit?: boolean;
  } = $props();

  const qc = useQueryClient();
  let queryKey = $derived(['documents', teamId, ownerKind, ownerId]);

  let docsQuery = createQuery(() => ({
    queryKey: ['documents', teamId, ownerKind, ownerId],
    queryFn: () => listDocuments(teamId, ownerKind, ownerId),
  }));

  let fileInput = $state<HTMLInputElement | null>(null);
  let uploading = $state(false);
  let dragOver = $state(false);
  let confirmingId = $state<string | null>(null);

  // Viewer dialog state.
  let viewerOpen = $state(false);
  let viewerDoc = $state<DocumentItem | null>(null);

  // lucide icons aren't Svelte-5 `Component`s; alias one for the return type.
  type LucideIcon = typeof FileIcon;

  function iconFor(doc: DocumentItem): LucideIcon {
    const ct = doc.content_type;
    if (ct === 'application/pdf') return FileText;
    if (ct.startsWith('image/')) return ImageIcon;
    if (ct === 'application/zip') return FileArchive;
    if (ct.includes('spreadsheet') || ct === 'text/csv' || ct === 'application/vnd.ms-excel' || ct === 'text/tab-separated-values')
      return FileSpreadsheet;
    if (ct.includes('presentation')) return Presentation;
    if (ct.startsWith('text/')) return FileText;
    return FileIcon;
  }

  async function handleFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    uploading = true;
    let ok = 0;
    for (const file of Array.from(files)) {
      try {
        await uploadDocument(teamId, ownerKind, ownerId, file);
        ok++;
      } catch (e) {
        pushToast('error', e instanceof Error ? e.message : `Failed to upload ${file.name}`);
      }
    }
    uploading = false;
    if (ok > 0) {
      qc.invalidateQueries({ queryKey });
      pushToast('success', ok === 1 ? 'Document uploaded' : `${ok} documents uploaded`);
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    dragOver = false;
    if (!canEdit) return;
    handleFiles(e.dataTransfer?.files ?? null);
  }

  async function remove(doc: DocumentItem) {
    try {
      await deleteDocument(teamId, doc.id);
      confirmingId = null;
      qc.invalidateQueries({ queryKey });
      pushToast('success', 'Document deleted');
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : 'Failed to delete document');
    }
  }

  function open(doc: DocumentItem) {
    viewerDoc = doc;
    viewerOpen = true;
  }

  let docs = $derived(docsQuery.data ?? []);
</script>

<section class="space-y-2">
  <div class="flex items-center justify-between">
    <h3 class="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Documents</h3>
    {#if canEdit}
      <Button variant="ghost" size="sm" onclick={() => fileInput?.click()} disabled={uploading}>
        <Upload class="mr-1 h-3.5 w-3.5" />
        {uploading ? 'Uploading…' : 'Upload'}
      </Button>
      <input
        bind:this={fileInput}
        type="file"
        multiple
        class="hidden"
        onchange={(e) => {
          handleFiles(e.currentTarget.files);
          e.currentTarget.value = '';
        }}
      />
    {/if}
  </div>

  {#if docsQuery.isLoading}
    <Skeleton class="h-16 w-full" />
  {:else if canEdit}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      role="group"
      class="rounded-md border border-dashed transition-colors {dragOver
        ? 'border-primary bg-primary/5'
        : 'border-border'}"
      ondragover={(e) => {
        e.preventDefault();
        dragOver = true;
      }}
      ondragleave={() => (dragOver = false)}
      ondrop={onDrop}
    >
      {@render docList()}
    </div>
  {:else}
    <div class="rounded-md border border-border">
      {@render docList()}
    </div>
  {/if}
</section>

{#snippet docList()}
  {#if docs.length === 0}
    <p class="p-4 text-sm italic text-muted-foreground">
      No documents yet.{#if canEdit} Drag files here or use Upload — datasheets, images, office files, S-parameters.{/if}
    </p>
  {:else}
    <ul class="divide-y divide-border">
      {#each docs as doc (doc.id)}
        {@const Icon = iconFor(doc)}
        <li class="flex items-center gap-3 px-3 py-2">
          <Icon class="h-5 w-5 shrink-0 text-muted-foreground" />
          <div class="min-w-0 flex-1">
            {#if doc.viewable}
              <button
                type="button"
                class="block max-w-full truncate text-left text-sm font-medium hover:underline"
                onclick={() => open(doc)}
                title={doc.filename}
              >
                {doc.filename}
              </button>
            {:else}
              <span class="block max-w-full truncate text-sm font-medium" title={doc.filename}>
                {doc.filename}
              </span>
            {/if}
            <p class="truncate text-xs text-muted-foreground">
              {formatBytes(doc.byte_size)}
              {#if doc.uploaded_by}· {doc.uploaded_by}{/if}
              {#if doc.uploaded_at}· <DateDisplay value={doc.uploaded_at} />{/if}
            </p>
          </div>

          <div class="flex shrink-0 items-center gap-1">
            {#if doc.viewable}
              <Button variant="ghost" size="icon" class="h-8 w-8" onclick={() => open(doc)} title="View">
                <Eye class="h-4 w-4" />
              </Button>
            {/if}
            <a
              class="inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              href={`/api/v1/teams/${teamId}/documents/${doc.id}/content?download=1`}
              title="Download"
            >
              <Download class="h-4 w-4" />
            </a>
            {#if canEdit}
              {#if confirmingId === doc.id}
                <Button variant="destructive" size="sm" class="h-8" onclick={() => remove(doc)}>Delete</Button>
                <Button variant="ghost" size="sm" class="h-8" onclick={() => (confirmingId = null)}>Cancel</Button>
              {:else}
                <Button
                  variant="ghost"
                  size="icon"
                  class="h-8 w-8 text-muted-foreground hover:text-destructive"
                  onclick={() => (confirmingId = doc.id)}
                  title="Delete"
                >
                  <Trash2 class="h-4 w-4" />
                </Button>
              {/if}
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
{/snippet}

<DocumentViewerDialog {teamId} doc={viewerDoc} open={viewerOpen} onOpenChange={(v) => (viewerOpen = v)} />

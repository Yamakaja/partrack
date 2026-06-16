<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { ExternalLink, Download, X } from 'lucide-svelte';
  import { documentContentUrl, type DocumentItem } from '$lib/api/documents.js';

  let {
    open,
    onOpenChange,
    teamId,
    doc,
  }: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    teamId: string;
    doc: DocumentItem | null;
  } = $props();

  // Inline (no download flag) for framing/embedding; the browser PDF viewer or
  // an <img> renders it. Forced inline only ever happens for PDF/image types.
  let inlineUrl = $derived(doc ? documentContentUrl(teamId, doc.id) : '');
  let downloadUrl = $derived(doc ? documentContentUrl(teamId, doc.id, { download: true }) : '');
  let isPdf = $derived(doc?.content_type === 'application/pdf');
  let isImage = $derived(doc?.content_type.startsWith('image/') ?? false);
  // Interactive HTML (e.g. BOMs) may run scripts but must stay isolated: the
  // server serves it with a CSP `sandbox` header (opaque origin, no egress), and
  // we frame it with sandbox="allow-scripts" — no allow-same-origin, so it can't
  // reach our cookies, API, or this DOM. The "New tab" link stays safe because
  // the same CSP header sandboxes direct navigation too.
  let isHtml = $derived(doc?.content_type.startsWith('text/html') ?? false);

  // The shared Button renders a <button>, so the link actions use styled anchors.
  const linkBtn =
    'inline-flex h-8 items-center justify-center gap-1 rounded-md border border-input bg-background px-3 text-xs font-medium transition-colors hover:bg-accent hover:text-accent-foreground';
</script>

<Dialog.Root {open} {onOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex h-[90vh] w-[92vw] max-w-6xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
    >
      <div class="flex items-center justify-between gap-4 border-b border-border px-4 py-3">
        <Dialog.Title class="truncate text-sm font-semibold">{doc?.filename ?? 'Document'}</Dialog.Title>
        <div class="flex shrink-0 items-center gap-2">
          {#if doc}
            <a class={linkBtn} href={inlineUrl} target="_blank" rel="noopener">
              <ExternalLink class="h-3.5 w-3.5" /> New tab
            </a>
            <a class={linkBtn} href={downloadUrl}>
              <Download class="h-3.5 w-3.5" /> Download
            </a>
          {/if}
          <Dialog.Close
            class="inline-flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <X class="h-4 w-4" />
          </Dialog.Close>
        </div>
      </div>

      <div class="flex-1 overflow-auto bg-muted/30">
        {#if doc && isPdf}
          <iframe src={inlineUrl} title={doc.filename} class="h-full w-full border-0"></iframe>
        {:else if doc && isHtml}
          <iframe
            src={inlineUrl}
            title={doc.filename}
            sandbox="allow-scripts"
            class="h-full w-full border-0 bg-white"
          ></iframe>
        {:else if doc && isImage}
          <div class="flex h-full w-full items-center justify-center p-4">
            <img src={inlineUrl} alt={doc.filename} class="max-h-full max-w-full object-contain" />
          </div>
        {:else}
          <div class="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
            <p class="text-sm">This file type can't be previewed in the browser.</p>
            {#if doc}
              <a class={linkBtn} href={downloadUrl}>
                <Download class="h-3.5 w-3.5" /> Download {doc.filename}
              </a>
            {/if}
          </div>
        {/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

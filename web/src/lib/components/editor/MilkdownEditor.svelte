<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Editor, rootCtx, defaultValueCtx, editorViewOptionsCtx } from '@milkdown/kit/core';
  import { commonmark } from '@milkdown/kit/preset/commonmark';
  import { gfm } from '@milkdown/kit/preset/gfm';
  import { history } from '@milkdown/kit/plugin/history';
  import { listener, listenerCtx } from '@milkdown/kit/plugin/listener';
  import { upload, uploadConfig, type Uploader } from '@milkdown/kit/plugin/upload';
  import { listItemBlockComponent } from '@milkdown/kit/component/list-item-block';
  import '@milkdown/kit/prose/view/style/prosemirror.css';
  import '@milkdown/kit/prose/tables/style/tables.css';
  import '@milkdown/kit/prose/gapcursor/style/gapcursor.css';
  import { directiveRemark, partChipNode, partChipView } from './partChip.svelte.js';
  import { slash, slashView } from './slash.svelte.js';
  import { uploadAttachment } from '$lib/api/attachments.js';

  let {
    teamId,
    value = '',
    onChange,
  }: { teamId: string; value?: string; onChange?: (markdown: string) => void } = $props();

  let host = $state<HTMLDivElement>();
  let editor: Editor | undefined;

  // Paste/drop image handler — Milkdown's upload plugin calls this with the
  // dropped/pasted files and expects ProseMirror image nodes back.
  const uploader: Uploader = async (files, schema) => {
    const nodes = [];
    for (let i = 0; i < files.length; i++) {
      const file = files.item(i);
      if (!file || !file.type.startsWith('image/')) continue;
      try {
        const a = await uploadAttachment(teamId, file);
        const node = schema.nodes.image.createAndFill({ src: a.url, alt: a.filename ?? file.name });
        if (node) nodes.push(node);
      } catch (e) {
        console.error('upload failed', e);
      }
    }
    return nodes;
  };

  onMount(async () => {
    editor = await Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, host!);
        ctx.set(defaultValueCtx, value);
        ctx.update(editorViewOptionsCtx, (prev) => ({
          ...prev,
          attributes: { class: 'milkdown-prose', spellcheck: 'false' },
        }));
        ctx.update(uploadConfig.key, (prev) => ({ ...prev, uploader }));
        ctx.set(slash.key, { view: slashView(teamId) });
        ctx.get(listenerCtx).markdownUpdated((_ctx, markdown) => {
          onChange?.(markdown);
        });
      })
      .use(commonmark)
      .use(gfm)
      .use(history)
      .use(listener)
      .use(upload)
      .use(listItemBlockComponent)
      .use(directiveRemark)
      .use(partChipNode)
      .use(partChipView)
      .use(slash)
      .create();
  });

  onDestroy(() => {
    editor?.destroy();
    editor = undefined;
  });
</script>

<div bind:this={host} class="milkdown-host"></div>

<style>
  .milkdown-host :global(.milkdown) {
    outline: none;
  }
  .milkdown-host :global(.milkdown-prose) {
    min-height: 16rem;
    padding: 1rem 1.25rem;
    outline: none;
    line-height: 1.6;
  }
  .milkdown-host :global(.milkdown-prose:focus) {
    outline: none;
  }
  .milkdown-host :global(.milkdown-prose h1) {
    font-size: 1.5rem;
    font-weight: 700;
    margin: 1rem 0 0.5rem;
  }
  .milkdown-host :global(.milkdown-prose h2) {
    font-size: 1.25rem;
    font-weight: 600;
    margin: 1rem 0 0.5rem;
  }
  .milkdown-host :global(.milkdown-prose h3) {
    font-size: 1.1rem;
    font-weight: 600;
    margin: 0.75rem 0 0.5rem;
  }
  .milkdown-host :global(.milkdown-prose p) {
    margin: 0.5rem 0;
  }
  /* Lists are rendered by listItemBlockComponent, which draws its own
     bullet/number/checkbox in a `.label` — so suppress native markers and lay
     the item out as a row (label beside content) instead of stacked blocks. */
  .milkdown-host :global(.milkdown-prose ul),
  .milkdown-host :global(.milkdown-prose ol) {
    list-style: none;
    padding-left: 0;
    margin: 0.5rem 0;
  }
  .milkdown-host :global(.milkdown-prose .list-item) {
    display: flex;
    align-items: flex-start;
    gap: 0.375rem;
  }
  .milkdown-host :global(.milkdown-prose .list-item .label-wrapper) {
    flex-shrink: 0;
    user-select: none;
    /* Center the bullet/checkbox against the first line of content (line-height
       1.6) instead of pinning it to the top of the line box. */
    display: flex;
    align-items: center;
    min-height: 1.6em;
  }
  /* The label is an inline span wrapping an SVG; inline SVGs carry baseline
     descender space that lifts the glyph above center. Block-level the icon so
     it centers exactly. */
  .milkdown-host :global(.milkdown-prose .list-item .label) {
    display: flex;
    align-items: center;
  }
  .milkdown-host :global(.milkdown-prose .list-item .label svg) {
    display: block;
  }
  .milkdown-host :global(.milkdown-prose .list-item .children) {
    flex: 1 1 0%;
    min-width: 0;
  }
  /* The content is wrapped one level deeper (.children > .content-dom > p), so
     zero the margins on the actual block content — otherwise the paragraph's
     top margin pushes the text down relative to the checkbox. */
  .milkdown-host :global(.milkdown-prose .list-item .children > *),
  .milkdown-host :global(.milkdown-prose .list-item .content-dom > *) {
    margin: 0;
  }
  .milkdown-host :global(.milkdown-prose blockquote) {
    border-left: 3px solid var(--border, #ccc);
    padding-left: 1rem;
    color: var(--muted-foreground, #666);
    margin: 0.5rem 0;
  }
  .milkdown-host :global(.milkdown-prose code) {
    font-family: ui-monospace, monospace;
    font-size: 0.875em;
    background: var(--muted, rgba(0, 0, 0, 0.06));
    padding: 0.1em 0.3em;
    border-radius: 0.25rem;
  }
  .milkdown-host :global(.milkdown-prose pre) {
    background: var(--muted, rgba(0, 0, 0, 0.06));
    padding: 0.75rem 1rem;
    border-radius: 0.375rem;
    overflow-x: auto;
    margin: 0.5rem 0;
  }
  .milkdown-host :global(.milkdown-prose pre code) {
    background: none;
    padding: 0;
  }
  .milkdown-host :global(.milkdown-prose table) {
    border-collapse: collapse;
    margin: 0.5rem 0;
    width: auto;
  }
  .milkdown-host :global(.milkdown-prose th),
  .milkdown-host :global(.milkdown-prose td) {
    border: 1px solid var(--border, #ccc);
    padding: 0.375rem 0.625rem;
    text-align: left;
  }
  .milkdown-host :global(.milkdown-prose th) {
    background: var(--muted, rgba(0, 0, 0, 0.04));
    font-weight: 600;
  }
  .milkdown-host :global(.milkdown-prose img) {
    max-width: 100%;
    border-radius: 0.375rem;
    margin: 0.5rem 0;
  }
  .milkdown-host :global(.milkdown-prose a) {
    color: var(--primary, #2563eb);
    text-decoration: underline;
  }
</style>

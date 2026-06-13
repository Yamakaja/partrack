<script lang="ts">
  import { Markdown, type Plugin } from 'svelte-exmarkdown';
  import { gfmPlugin } from 'svelte-exmarkdown/gfm';
  import remarkDirective from 'remark-directive';
  import rehypeSanitize, { defaultSchema } from 'rehype-sanitize';
  import { remarkManifest } from './remarkManifest.js';
  import ObjectChipView from './ObjectChipView.svelte';
  import type { NoteManifest } from '$lib/api/notes.js';

  let { body, manifest }: { body: string; manifest: NoteManifest; teamId?: string } = $props();

  // The viewer renders stored markdown another teammate wrote — a stored-XSS
  // vector — so sanitize is mandatory. Extend the (GitHub) default schema to
  // allow our <obj-link> element and the props remarkManifest bakes onto it.
  const schema = {
    ...defaultSchema,
    tagNames: [...(defaultSchema.tagNames ?? []), 'obj-link'],
    attributes: {
      ...defaultSchema.attributes,
      'obj-link': ['kind', 'label', 'href', 'status', 'exists'],
      img: [...(defaultSchema.attributes?.img ?? []), 'width', 'height'],
    },
  };

  // Rebuilt when the manifest changes (e.g. after a save) so chips re-resolve.
  let plugins = $derived<Plugin[]>([
    gfmPlugin(),
    { remarkPlugin: remarkDirective },
    { remarkPlugin: [remarkManifest, { manifest }] },
    { rehypePlugin: [rehypeSanitize, schema] },
    { renderer: { 'obj-link': ObjectChipView } },
  ]);
</script>

<div class="partrack-prose text-sm">
  <Markdown md={body} {plugins} />
</div>

<style>
  .partrack-prose :global(h1) {
    font-size: 1.4rem;
    font-weight: 700;
    margin: 1rem 0 0.5rem;
  }
  .partrack-prose :global(h2) {
    font-size: 1.2rem;
    font-weight: 600;
    margin: 1rem 0 0.5rem;
  }
  .partrack-prose :global(h3) {
    font-size: 1.05rem;
    font-weight: 600;
    margin: 0.75rem 0 0.5rem;
  }
  .partrack-prose :global(p) {
    margin: 0.5rem 0;
    line-height: 1.6;
  }
  .partrack-prose :global(ul) {
    list-style: disc;
    padding-left: 1.5rem;
    margin: 0.5rem 0;
  }
  .partrack-prose :global(ol) {
    list-style: decimal;
    padding-left: 1.5rem;
    margin: 0.5rem 0;
  }
  .partrack-prose :global(li.task-list-item) {
    list-style: none;
    margin-left: -1.25rem;
  }
  .partrack-prose :global(li.task-list-item input) {
    margin-right: 0.4em;
    vertical-align: middle;
  }
  .partrack-prose :global(blockquote) {
    border-left: 3px solid var(--border, #ccc);
    padding-left: 1rem;
    color: var(--muted-foreground, #666);
    margin: 0.5rem 0;
  }
  .partrack-prose :global(code) {
    font-family: ui-monospace, monospace;
    font-size: 0.875em;
    background: var(--muted, rgba(0, 0, 0, 0.06));
    padding: 0.1em 0.3em;
    border-radius: 0.25rem;
  }
  .partrack-prose :global(pre) {
    background: var(--muted, rgba(0, 0, 0, 0.06));
    padding: 0.75rem 1rem;
    border-radius: 0.375rem;
    overflow-x: auto;
    margin: 0.5rem 0;
  }
  .partrack-prose :global(pre code) {
    background: none;
    padding: 0;
  }
  .partrack-prose :global(table) {
    border-collapse: collapse;
    margin: 0.5rem 0;
  }
  .partrack-prose :global(th),
  .partrack-prose :global(td) {
    border: 1px solid var(--border, #ccc);
    padding: 0.375rem 0.625rem;
    text-align: left;
  }
  .partrack-prose :global(th) {
    background: var(--muted, rgba(0, 0, 0, 0.04));
    font-weight: 600;
  }
  .partrack-prose :global(img) {
    max-width: 100%;
    border-radius: 0.375rem;
    margin: 0.5rem 0;
  }
  .partrack-prose :global(a) {
    color: var(--primary, #2563eb);
    text-decoration: underline;
  }
</style>

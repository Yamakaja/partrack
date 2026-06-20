<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Markdown, type Plugin } from 'svelte-exmarkdown';
  import { gfmPlugin } from 'svelte-exmarkdown/gfm';
  import { X } from 'lucide-svelte';
  import { CHAPTERS, chapterContent, chapterTitle } from '$lib/help/chapters.js';
  import { helpStore } from '$lib/stores/help.svelte.js';

  // Give headings GitHub-style id attributes so in-guide `#anchor` links can
  // scroll to them. Inline (no extra dependency); guide content is authored
  // in-repo and trusted, so it renders without sanitization.
  function slugify(s: string): string {
    // Mirror github-slugger: lowercase, drop punctuation (keep spaces + hyphens),
    // then turn each remaining space into a hyphen WITHOUT collapsing runs, so
    // anchors like `ldap--active-directory-assignment` (from "LDAP / Active …")
    // match the cross-links the chapters were written with.
    return s
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9 \-]/g, '')
      .replace(/ /g, '-');
  }
  function nodeText(node: any): string {
    if (node.type === 'text') return node.value ?? '';
    if (node.children) return node.children.map(nodeText).join('');
    return '';
  }
  function rehypeHeadingIds() {
    return (tree: any) => {
      const used = new Set<string>();
      const walk = (node: any) => {
        if (node.type === 'element' && /^h[1-6]$/.test(node.tagName)) {
          const base = slugify(nodeText(node));
          if (base) {
            let id = base;
            let n = 1;
            while (used.has(id)) id = `${base}-${n++}`;
            used.add(id);
            node.properties = node.properties ?? {};
            if (!node.properties.id) node.properties.id = id;
          }
        }
        node.children?.forEach(walk);
      };
      walk(tree);
    };
  }

  const plugins: Plugin[] = [gfmPlugin(), { rehypePlugin: rehypeHeadingIds }];

  let body = $derived(chapterContent(helpStore.slug));
  let title = $derived(chapterTitle(helpStore.slug));

  let contentEl = $state<HTMLDivElement | null>(null);

  function select(slug: string) {
    helpStore.slug = slug;
    if (contentEl) contentEl.scrollTop = 0;
  }

  // Content renders asynchronously after a chapter switch, so retry across a few
  // frames until the target heading exists.
  function scrollToAnchor(id: string, attempts = 12) {
    requestAnimationFrame(() => {
      const el = contentEl?.querySelector(`#${CSS.escape(id)}`);
      if (el) {
        el.scrollIntoView({ block: 'start' });
      } else if (attempts > 0) {
        scrollToAnchor(id, attempts - 1);
      }
    });
  }

  // Intercept link clicks inside the rendered markdown: cross-chapter (`*.md`)
  // links switch chapters, `#anchor` links scroll in-place, and external links
  // open in a new tab — none of them should navigate the SPA away.
  function onContentClick(e: MouseEvent) {
    const anchor = (e.target as HTMLElement).closest('a');
    if (!anchor) return;
    const href = anchor.getAttribute('href');
    if (!href) return;

    if (/^https?:\/\//.test(href)) {
      e.preventDefault();
      window.open(href, '_blank', 'noopener,noreferrer');
      return;
    }
    if (href.startsWith('#')) {
      e.preventDefault();
      scrollToAnchor(href.slice(1));
      return;
    }
    const m = href.match(/^([a-z0-9-]+)\.md(?:#(.+))?$/i);
    if (m && CHAPTERS.some((c) => c.slug === m[1])) {
      e.preventDefault();
      select(m[1]);
      if (m[2]) scrollToAnchor(m[2]);
    }
  }

  // Attach the click handler imperatively (rather than onclick on a div) to keep
  // the markup free of non-interactive-element a11y warnings.
  $effect(() => {
    const el = contentEl;
    if (!el) return;
    el.addEventListener('click', onContentClick);
    return () => el.removeEventListener('click', onContentClick);
  });
</script>

<Dialog.Root open={helpStore.open} onOpenChange={(v) => (helpStore.open = v)}>
  <Dialog.Portal>
    <Dialog.Overlay
      class="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <Dialog.Content
      class="fixed inset-y-0 right-0 z-50 flex w-full max-w-3xl border-l border-border bg-background shadow-xl duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right"
    >
      <!-- Table of contents -->
      <nav class="w-56 shrink-0 overflow-y-auto border-r border-border bg-muted/30 p-3">
        <p class="px-2 pb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          User guide
        </p>
        <ul class="space-y-0.5">
          {#each CHAPTERS as ch}
            <li>
              <button
                onclick={() => select(ch.slug)}
                class="w-full rounded px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent/60 {helpStore.slug ===
                ch.slug
                  ? 'bg-accent font-medium text-foreground'
                  : 'text-muted-foreground'}"
              >
                {ch.title}
              </button>
            </li>
          {/each}
        </ul>
      </nav>

      <!-- Chapter content -->
      <div class="flex min-w-0 flex-1 flex-col">
        <div class="flex items-center justify-between border-b border-border px-6 py-3">
          <Dialog.Title class="text-base font-semibold">{title}</Dialog.Title>
          <Dialog.Close
            class="text-muted-foreground hover:text-foreground rounded p-1 hover:bg-accent/50"
            aria-label="Close help"
          >
            <X class="size-4" />
          </Dialog.Close>
        </div>
        <Dialog.Description class="sr-only">partrack user guide — {title}</Dialog.Description>
        <div bind:this={contentEl} class="help-prose flex-1 overflow-y-auto px-6 py-5 text-sm">
          <Markdown md={body} {plugins} />
        </div>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  /* The drawer header already shows the chapter title, so hide the body H1
     (it stays in the markdown file for the external mdBook site). */
  .help-prose :global(h1) {
    display: none;
  }
  .help-prose :global(h2) {
    font-size: 1.15rem;
    font-weight: 600;
    margin: 1.25rem 0 0.5rem;
  }
  .help-prose :global(h3) {
    font-size: 1rem;
    font-weight: 600;
    margin: 1rem 0 0.4rem;
  }
  .help-prose :global(p) {
    margin: 0.5rem 0;
    line-height: 1.65;
  }
  .help-prose :global(ul) {
    list-style: disc;
    padding-left: 1.4rem;
    margin: 0.5rem 0;
  }
  .help-prose :global(ol) {
    list-style: decimal;
    padding-left: 1.4rem;
    margin: 0.5rem 0;
  }
  .help-prose :global(li) {
    margin: 0.2rem 0;
    line-height: 1.6;
  }
  .help-prose :global(a) {
    color: var(--primary, #2563eb);
    text-decoration: underline;
  }
  .help-prose :global(strong) {
    font-weight: 600;
  }
  .help-prose :global(code) {
    font-family: ui-monospace, monospace;
    font-size: 0.85em;
    background: var(--muted, rgba(0, 0, 0, 0.06));
    padding: 0.1em 0.3em;
    border-radius: 0.25rem;
  }
  .help-prose :global(blockquote) {
    border-left: 3px solid var(--border, #ccc);
    padding-left: 1rem;
    color: var(--muted-foreground, #666);
    margin: 0.75rem 0;
  }
  .help-prose :global(table) {
    border-collapse: collapse;
    margin: 0.75rem 0;
    width: 100%;
  }
  .help-prose :global(th),
  .help-prose :global(td) {
    border: 1px solid var(--border, #ccc);
    padding: 0.375rem 0.625rem;
    text-align: left;
    vertical-align: top;
  }
  .help-prose :global(th) {
    background: var(--muted, rgba(0, 0, 0, 0.04));
    font-weight: 600;
  }
  .help-prose :global(hr) {
    border: none;
    border-top: 1px solid var(--border, #ccc);
    margin: 1.25rem 0;
  }
</style>

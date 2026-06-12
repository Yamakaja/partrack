<script lang="ts">
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import MilkdownEditor from '$lib/components/editor/MilkdownEditor.svelte';

  let { teamId }: { teamId: string } = $props();

  // Seed content exercises every render path: headings, lists, a GFM table, a
  // task list, code, a blockquote, and a sample part chip (the custom node).
  const initial = `# Notes editor spike

This is a **Milkdown** spike. Type \`/\` for the slash menu, paste or drop an image,
and edit the table below.

## Part reference

The MCU is :part[STM32F407VGT6]{#00000000-0000-0000-0000-000000000000} — that chip
is a live node, not text.

## Parameter comparison

| Parameter | Option A | Option B |
| --------- | -------- | -------- |
| Vcc       | 3.3 V    | 5 V      |
| Package   | LQFP-100 | LQFP-64  |

## Todo

- [x] Mount Milkdown in Svelte 5
- [x] Custom part-chip node
- [ ] Wire to backend notes

> Tip: the markdown source on the right updates live as you type.
`;

  let markdown = $state(initial);
</script>

<div class="flex h-full flex-col">
  <Topbar title="Notes editor spike" breadcrumbs={[{ label: 'Team', href: `/teams/${teamId}` }]} />
  <div class="flex-1 overflow-auto p-6">
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <div>
        <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Editor
        </h3>
        <div class="rounded-md border border-border bg-card">
          <MilkdownEditor {teamId} value={initial} onChange={(md) => (markdown = md)} />
        </div>
      </div>
      <div>
        <h3 class="mb-2 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Markdown source (stored verbatim)
        </h3>
        <pre class="overflow-auto rounded-md border border-border bg-muted/40 p-4 text-xs leading-relaxed">{markdown}</pre>
      </div>
    </div>
  </div>
</div>

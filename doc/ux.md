# UX Patterns

This document is the canonical reference for visual and interaction patterns in the partrack frontend. Follow these patterns when building new pages or components to maintain consistency.

---

## Page Layout

Every page uses a full-height flex column with a sticky topbar and a scrollable content area.

```svelte
<div class="flex flex-col h-full">
  <Topbar title="Page Title" breadcrumbs={[...]} {actions} />
  <div class="flex-1 overflow-auto p-6 space-y-6">
    <!-- metadata card, then sections -->
  </div>
</div>
```

Within the content area, group related content into named sections:

```svelte
<section class="space-y-2">
  <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Section Title</h3>
  <!-- section content -->
</section>
```

---

## Topbar & Actions

The `Topbar` component (`web/src/lib/components/layout/Topbar.svelte`) renders breadcrumbs and page title on the left (inline, same size, breadcrumbs dimmed), optional status badges immediately after the title, and action buttons on the right.

```svelte
{#snippet badges()}
  <LifecycleBadge status={partQuery.data?.lifecycle_status} />
  <Badge variant="outline" class="capitalize">{partQuery.data?.scope}</Badge>
{/snippet}

{#snippet actions()}
  <Button variant="outline" size="sm" onclick={openEdit}>
    <Pencil class="h-4 w-4 mr-1" /> Edit
  </Button>
  <Button size="sm" onclick={doImport}>
    <Upload class="h-4 w-4 mr-1" /> Import BOM
  </Button>
{/snippet}

<Topbar title="Part Detail" breadcrumbs={[{ label: 'Parts', href: '/parts' }]} {badges} {actions} />
```

Rules:
- Breadcrumbs list **ancestor pages only** — the current page is shown as `title`, not as a breadcrumb entry.
- Primary entity status badges (lifecycle, stock level, project/revision status) go in the `badges` snippet so they appear at eye level next to the page name.
- The `badges` snippet renders inside the same flex row as the title; the `Badge` component's own `text-xs` overrides the parent `text-lg`, so no size reset is needed.
- Guard badge content with `{#if query.data}` — the Topbar renders before the query resolves.
- All action buttons use `size="sm"`.
- The primary action (one per page, if any) uses a solid button; secondary actions use `variant="outline"`.
- Icons are 16 px (`h-4 w-4`) with `mr-1` before the label.
- Actions are conditionally rendered based on entity state or user role.

Reference: [PartDetail.svelte](web/src/routes/team/PartDetail.svelte), [ProjectDetail.svelte](web/src/routes/team/ProjectDetail.svelte), [OrderDetail.svelte](web/src/routes/team/OrderDetail.svelte)

---

## Entity Metadata Card

Every entity detail page opens with a metadata card at the top of the content area, before any sections. The card is the primary read surface for the entity.

Use the shared `MetadataCard.svelte` component — do not inline the card structure:

```svelte
<MetadataCard title={entity.name} titleClass="font-mono" description={entity.description}>
  {#snippet badges()}
    <Badge variant="outline">status</Badge>
    <!-- additional inline badges -->
  {/snippet}

  {#snippet right()}
    <!-- optional: edit button or status badge on the right of the title row -->
    <Button variant="outline" size="sm" onclick={openEdit}>Edit</Button>
  {/snippet}

  <!-- optional body: key-value metadata grid -->
  <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm">
    <div><span class="text-muted-foreground">Field:</span> <span class="font-medium">{value ?? '—'}</span></div>
  </div>

  {#snippet footer()}
    <!-- optional: stats section, rendered below a border-t separator -->
    <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
      <div>
        <p class="text-xs text-muted-foreground mb-0.5">Quantity on Hand</p>
        <p class="text-2xl font-bold tabular-nums">{qty}</p>
      </div>
    </div>
  {/snippet}
</MetadataCard>
```

Props:

| Prop | Type | Purpose |
|---|---|---|
| `title` | `string` | Main heading |
| `description` | `string?` | Subtitle shown below the heading (omit to hide) |
| `titleClass` | `string?` | Extra classes on `<h2>` — use `"font-mono"` for MPN / code identifiers |
| `badges` | snippet | Inline badges rendered after the title text — use sparingly; primary entity status badges belong in the Topbar `badges` slot instead |
| `right` | snippet | Content on the right of the title row (edit button, etc.) |
| children | snippet | Optional body content (key-value grid, etc.) |
| `footer` | snippet | Optional section rendered below a `border-t` separator |

Rules:
- Use `—` as the placeholder for null/empty optional fields.
- The key-value grid uses a 2-column layout; the stats section adapts to 4 columns on larger screens.
- Stat values use `tabular-nums` so numbers align vertically.
- The `footer` snippet's border-t separator is added automatically — do not add it inside the snippet.

Reference: [MetadataCard.svelte](web/src/lib/components/common/MetadataCard.svelte), [PartDetail.svelte](web/src/routes/team/PartDetail.svelte), [ProjectDetail.svelte](web/src/routes/team/ProjectDetail.svelte)

---

## Modal Dialogs

All dialogs use the bits-ui `Dialog.Root` primitive (or the shared `Dialog.svelte` wrapper). This provides Escape-to-close automatically.

**Sizing tiers** — use the smallest tier that fits the content:

| Class | Use case |
|---|---|
| `max-w-sm` | Confirmations, single-field actions |
| `max-w-lg` | Standard forms (default) |
| `max-w-md` | Progress / status modals |
| `max-w-2xl` | Review dialogs with tables |
| `w-[90vw] max-h-[85vh]` | Large data previews (e.g. CSV column mapping) |

**Standard dialog structure:**

```svelte
<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Dialog Title</Dialog.Title>
      <form onsubmit={handleSubmit} class="space-y-3">
        <div class="space-y-1">
          <Label for="field">Field Label</Label>
          <Input id="field" bind:value={fieldValue} />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Dialog.Close>
            <Button variant="outline" type="button">Cancel</Button>
          </Dialog.Close>
          <Button type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
```

**Large dialogs with scrollable content** (tables, long lists):

```svelte
<Dialog.Content class="fixed left-1/2 top-1/2 z-50 flex w-[90vw] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg" style="max-height: 85vh;">
  <!-- Header: shrink-0 so it never scrolls away -->
  <div class="shrink-0 p-6 pb-0">...</div>
  <!-- Scrollable body -->
  <div class="min-h-0 flex-1 overflow-auto p-6">...</div>
  <!-- Footer: shrink-0 -->
  <div class="shrink-0 p-6 pt-0 flex justify-end gap-2">...</div>
</Dialog.Content>
```

Rules:
- Dialog size **must not change** when advancing between steps or loading data. Fix the modal dimensions upfront.
- The submit button shows a past-progressive label while pending ("Saving…", "Importing…") and is disabled.
- Cancel is always `variant="outline"` and wrapped in `Dialog.Close` so Escape and the button do the same thing.

Reference: [AddListingDialog.svelte](web/src/lib/components/parts/AddListingDialog.svelte), [ConfirmDialog.svelte](web/src/lib/components/common/ConfirmDialog.svelte), [BOMImportDialog.svelte](web/src/lib/components/bom/BOMImportDialog.svelte), [Dialog.svelte](web/src/lib/components/ui/dialog/)

---

## Multi-Step Dialogs

Use a single union-typed state variable to track the current step. Render each step with `{#if}/{:else if}` inside a fixed-size dialog.

```svelte
let step = $state<'upload' | 'map' | 'confirm'>('upload');
```

```svelte
<!-- Step indicator in header -->
<div class="flex items-center gap-1 text-sm text-muted-foreground">
  <span class={step === 'upload' ? 'font-semibold text-foreground' : ''}>1. Upload</span>
  <span>›</span>
  <span class={step === 'map' ? 'font-semibold text-foreground' : ''}>2. Map Columns</span>
  <span>›</span>
  <span class={step === 'confirm' ? 'font-semibold text-foreground' : ''}>3. Confirm</span>
</div>

<!-- Step content (fixed-height container) -->
<div class="min-h-[320px]">
  {#if step === 'upload'}
    ...
  {:else if step === 'map'}
    ...
  {:else}
    ...
  {/if}
</div>
```

Rules:
- Navigation logic lives in the advance function, not in the template — the function decides which step comes next (including skipping irrelevant steps).
- Use `$effect` to reset sub-component state (search results, selections) when the step or current index changes.
- Sequential single-item review (e.g. BOM review): use `let currentIndex = $state(0)` and increment on advance; show "Item N of M" progress.

Reference: [BOMImportDialog.svelte](web/src/lib/components/bom/BOMImportDialog.svelte), [BOMReviewDialog.svelte](web/src/lib/components/bom/BOMReviewDialog.svelte)

---

## Keyboard Interactions

**Enter submits; Escape cancels.**

For any input that triggers an action, attach an `onkeydown` handler:

```svelte
<Input
  bind:value={query}
  onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); doSearch(); } }}
/>
```

For inline table edits where both Enter and Escape are meaningful:

```svelte
function handleKey(e: KeyboardEvent) {
  if (e.key === 'Enter') saveEdit();
  else if (e.key === 'Escape') cancelEdit();
}
```

Rules:
- bits-ui `Dialog.Root` handles Escape automatically — no manual handler needed in dialogs.
- Inline edits in table cells **must** handle Escape manually since they are not inside a Dialog.
- Always call `e.preventDefault()` on Enter to prevent accidental form submission or newlines.

Reference: [ParameterTable.svelte](web/src/lib/components/parts/ParameterTable.svelte), [Cart.svelte](web/src/routes/team/Cart.svelte)

---

## Search Patterns

There are two distinct search patterns depending on whether the search hits the local dataset or a remote API.

### Local / In-Memory Search

Auto-triggers on input with a 300 ms debounce. No button required.

Use the shared `SearchInput.svelte` component when possible:

```svelte
<SearchInput placeholder="Filter parts…" onchange={(q) => (filter = q)} />
```

Or replicate the pattern manually:

```svelte
let timer: ReturnType<typeof setTimeout>;
function onInput(value: string) {
  clearTimeout(timer);
  timer = setTimeout(() => runSearch(value), 300);
}
```

### Remote API Search

Requires explicit user action: a button click **or** Enter in the search field. Never auto-fires on keystroke.

```svelte
<div class="flex gap-2">
  <Input
    bind:value={query}
    placeholder="Search distributor…"
    onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); doSearch(); } }}
  />
  <Button onclick={doSearch} disabled={searching}>
    {searching ? 'Searching…' : 'Search'}
  </Button>
</div>

{#if results.length > 0}
  <div class="max-h-64 overflow-y-auto border border-border rounded-md divide-y divide-border">
    {#each results as r (r.id)}
      <button class="w-full text-left px-3 py-2 hover:bg-muted" onclick={() => select(r)}>
        {r.label}
      </button>
    {/each}
  </div>
{/if}
```

Rules:
- Show a loading state on the button while the request is in flight; disable the button to prevent duplicate requests.
- Display results in a `max-h-*` scrollable container so the page layout is not disrupted by large result sets.
- Use `max-h-48` for compact contexts (inline panels), `max-h-64` for standard contexts.

Reference: [SearchInput.svelte](web/src/lib/components/common/SearchInput.svelte), [AddListingDialog.svelte](web/src/lib/components/parts/AddListingDialog.svelte), [BOMReviewDialog.svelte](web/src/lib/components/bom/BOMReviewDialog.svelte)

---

## Badges & Status Indicators

Use the `Badge` component for categorical status values. Available variants: `default`, `outline`, `success`, `destructive`, `warning`.

For domain-specific status types use the dedicated badge components:
- `LifecycleBadge` — part lifecycle status (Active, NRND, Obsolete, …)
- `StockBadge` — quantity on hand with contextual color

**Placement:** Primary entity status badges (lifecycle, stock level, revision readiness, project status) go in the Topbar `badges` snippet so they appear inline with the page name. Badges inside `MetadataCard` are reserved for supplemental status that is only relevant in the card's context.

Multiple badges on one line:

```svelte
<div class="flex items-center gap-2">
  <LifecycleBadge status={part.lifecycle_status} />
  <Badge variant="outline" class="capitalize">{part.scope}</Badge>
  <StockBadge quantity_on_hand={part.quantity_on_hand ?? 0} />
</div>
```

Secondary metadata (not a badge — just informational text) uses muted foreground with ` · ` as a separator:

```svelte
<span class="text-sm text-muted-foreground">
  Imported by {rev.imported_by_display_name} · {formatDate(rev.created_at)}
</span>
```

---

## Empty & Loading States

**Empty lists:** Use the `EmptyState` component with an icon, a heading, and an optional call-to-action button.

**Skeletons:** Use the `Skeleton` component sized to match the approximate dimensions of the content that will appear.

**Loading buttons:** Disable the button and change its label to a past-progressive verb:

| State | Label |
|---|---|
| Saving a form | "Saving…" |
| Running a search | "Searching…" |
| Uploading / importing | "Importing…" |
| Refreshing data | "Refreshing…" |

Reference: [EmptyState.svelte](web/src/lib/components/common/EmptyState.svelte), [Skeleton.svelte](web/src/lib/components/ui/skeleton/)

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listCategories } from '$lib/api/categories.js';
  import { listParameterDefinitions, updateCategoryParams } from '$lib/api/admin.js';
  import type { Category } from '$lib/types/categories.js';
  import type { ParameterDefinition } from '$lib/types/parts.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ChevronUp, ChevronDown, X } from 'lucide-svelte';

  const qc = useQueryClient();

  const categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: () => listCategories(),
  }));

  const defsQuery = createQuery(() => ({
    queryKey: ['admin', 'parameter-definitions'],
    queryFn: () => listParameterDefinitions(),
  }));

  // Slug → definition, for showing human names next to the raw slugs.
  const defBySlug = $derived.by(() => {
    const m = new Map<string, ParameterDefinition>();
    for (const d of defsQuery.data ?? []) m.set(d.slug, d);
    return m;
  });
  const defsSorted = $derived(
    [...(defsQuery.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name)),
  );

  function paramLabel(slug: string): string {
    const d = defBySlug.get(slug);
    return d ? d.display_name : slug;
  }
  function isConfigured(c: Category): boolean {
    return c.value_param != null || c.primary_params.length > 0;
  }

  let search = $state('');
  let showAll = $state(false);

  const visible = $derived.by(() => {
    const q = search.trim().toLowerCase();
    const cats = categoriesQuery.data ?? [];
    return cats.filter((c) => {
      if (q) return `${c.display_name} ${c.slug}`.toLowerCase().includes(q);
      return showAll || isConfigured(c);
    });
  });
  const configuredCount = $derived((categoriesQuery.data ?? []).filter(isConfigured).length);

  // ── Edit dialog ───────────────────────────────────────────────────────────────
  let editOpen = $state(false);
  let editCat = $state<Category | null>(null);
  let editValueParam = $state('');
  let editParams = $state<string[]>([]);
  let editGenericCode = $state('');
  let saving = $state(false);

  // Definitions not already in the primary list, for the "add" picker.
  const addable = $derived(defsSorted.filter((d) => !editParams.includes(d.slug)));

  function openEdit(c: Category) {
    editCat = c;
    editValueParam = c.value_param ?? '';
    editParams = [...c.primary_params];
    editGenericCode = c.generic_code ?? '';
    editOpen = true;
  }

  function addParam(slug: string) {
    if (slug && !editParams.includes(slug)) editParams = [...editParams, slug];
  }
  function removeParam(i: number) {
    editParams = editParams.filter((_, idx) => idx !== i);
  }
  function moveParam(i: number, dir: -1 | 1) {
    const j = i + dir;
    if (j < 0 || j >= editParams.length) return;
    const next = [...editParams];
    [next[i], next[j]] = [next[j], next[i]];
    editParams = next;
  }

  // Picking a magnitude implies it is a primary parameter — keep it in the list.
  function onValueParamChange(slug: string) {
    editValueParam = slug;
    if (slug && !editParams.includes(slug)) editParams = [slug, ...editParams];
  }

  async function save() {
    if (!editCat) return;
    saving = true;
    try {
      // Generic code is only meaningful with a value parameter; clear it otherwise
      // (the server enforces the same dependency).
      const code = editValueParam ? editGenericCode.trim().toUpperCase() : '';
      await updateCategoryParams(editCat.id, {
        value_param: editValueParam || null,
        primary_params: editParams,
        generic_code: code || null,
      });
      editOpen = false;
      qc.invalidateQueries({ queryKey: ['categories'] });
      pushToast('success', `Updated "${editCat.display_name}"`);
    } catch (err: any) {
      pushToast('error', 'Failed to update category', err.message);
    } finally {
      saving = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Category Parameters" />

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <p class="text-sm text-muted-foreground max-w-3xl">
      Curate, per category, the <strong>value parameter</strong> (the magnitude a bare value field encodes —
      e.g. resistance for resistors; leave blank for non-parametric categories), the ordered list of
      <strong>primary parameters</strong> that matter, and an optional <strong>generic code</strong> that enables
      batch / generic-part creation for the category. These drive generic-part interpretation during BOM import,
      and will be supplied to the LLM parser as the per-category extraction target.
    </p>

    <div class="flex items-center gap-3">
      <Input bind:value={search} placeholder="Search categories…" class="max-w-xs" />
      <label class="flex items-center gap-1.5 text-sm text-muted-foreground">
        <input type="checkbox" bind:checked={showAll} class="rounded border-input" />
        Show all ({categoriesQuery.data?.length ?? 0})
      </label>
      <span class="ml-auto text-xs text-muted-foreground">{configuredCount} configured</span>
    </div>

    {#if categoriesQuery.isPending || defsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if categoriesQuery.isError}
      <div class="text-sm text-destructive">Error: {categoriesQuery.error.message}</div>
    {:else}
      {#if visible.length === 0}
        <div class="text-sm text-muted-foreground py-8 text-center">
          {search ? 'No categories match your search.' : 'No configured categories yet — search to find one to set up.'}
        </div>
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Category</TableHead>
              <TableHead>Value parameter</TableHead>
              <TableHead>Generic</TableHead>
              <TableHead>Primary parameters</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each visible as c (c.id)}
              <TableRow>
                <TableCell>
                  <div class="font-medium">{c.display_name}</div>
                  <div class="text-muted-foreground text-xs font-mono">{c.slug}</div>
                </TableCell>
                <TableCell>
                  {#if c.value_param}
                    <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-primary/10 text-primary">
                      {paramLabel(c.value_param)}
                    </span>
                  {:else}
                    <span class="text-muted-foreground text-xs">—</span>
                  {/if}
                </TableCell>
                <TableCell>
                  {#if c.generic_code}
                    <span class="inline-flex items-center rounded px-1.5 py-0.5 text-xs font-mono font-medium bg-primary/10 text-primary">
                      {c.generic_code}
                    </span>
                  {:else}
                    <span class="text-muted-foreground text-xs">—</span>
                  {/if}
                </TableCell>
                <TableCell>
                  {#if c.primary_params.length > 0}
                    <div class="flex flex-wrap gap-1">
                      {#each c.primary_params as slug (slug)}
                        <span class="inline-block rounded px-1.5 py-0.5 text-xs bg-muted text-muted-foreground" title={slug}>
                          {paramLabel(slug)}
                        </span>
                      {/each}
                    </div>
                  {:else}
                    <span class="text-muted-foreground text-xs">—</span>
                  {/if}
                </TableCell>
                <TableCell class="text-right">
                  <Button variant="ghost" size="sm" onclick={() => openEdit(c)}>Edit</Button>
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>

<!-- Edit dialog -->
<Dialog.Root bind:open={editOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Edit Category Parameters</Dialog.Title>
      {#if editCat}
        <p class="text-xs text-muted-foreground mb-4">
          {editCat.display_name} <span class="font-mono">({editCat.slug})</span>
        </p>

        <div class="space-y-4">
          <!-- Value parameter -->
          <div class="space-y-1">
            <Label for="value-param">Value parameter</Label>
            <select
              id="value-param"
              value={editValueParam}
              onchange={(e: Event) => onValueParamChange((e.currentTarget as HTMLSelectElement).value)}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            >
              <option value="">— none (not parametric) —</option>
              {#each defsSorted as d (d.id)}
                <option value={d.slug}>{d.display_name} ({d.slug}{d.unit_symbol ? ` · ${d.unit_symbol}` : ''})</option>
              {/each}
            </select>
            <p class="text-xs text-muted-foreground">The magnitude a bare value (e.g. "10k") maps to. Added to primary parameters automatically.</p>
          </div>

          <!-- Generic part code -->
          <div class="space-y-1">
            <Label for="generic-code">Generic part code</Label>
            <Input
              id="generic-code"
              value={editGenericCode}
              oninput={(e: Event) => (editGenericCode = (e.currentTarget as HTMLInputElement).value.toUpperCase())}
              placeholder="e.g. RES"
              maxlength={8}
              disabled={!editValueParam}
              class="font-mono uppercase max-w-[10rem]"
            />
            <p class="text-xs text-muted-foreground">
              {#if editValueParam}
                Enables batch / generic-part creation. Used to synthesize MPNs like
                <span class="font-mono">GEN-{editGenericCode || 'RES'}-4K7-0402-1%</span>. Leave blank to disable.
              {:else}
                Set a value parameter first — generic parts need a magnitude to synthesize.
              {/if}
            </p>
          </div>

          <!-- Primary parameters (ordered) -->
          <div class="space-y-1">
            <Label>Primary parameters <span class="text-muted-foreground font-normal">(ordered by importance)</span></Label>
            {#if editParams.length === 0}
              <p class="text-xs text-muted-foreground py-1">None yet — add parameters below.</p>
            {:else}
              <ul class="divide-y divide-border rounded-md border border-border">
                {#each editParams as slug, i (slug)}
                  <li class="flex items-center gap-2 px-2 py-1.5 text-sm">
                    <span class="w-5 text-right text-xs text-muted-foreground tabular-nums">{i + 1}.</span>
                    <span class="font-medium">{paramLabel(slug)}</span>
                    <span class="text-xs text-muted-foreground font-mono">{slug}</span>
                    {#if slug === editValueParam}
                      <span class="rounded-full bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium text-primary">value</span>
                    {/if}
                    <div class="ml-auto flex items-center gap-0.5">
                      <button type="button" class="rounded p-1 text-muted-foreground hover:bg-accent disabled:opacity-30" disabled={i === 0} onclick={() => moveParam(i, -1)} aria-label="Move up">
                        <ChevronUp class="h-3.5 w-3.5" />
                      </button>
                      <button type="button" class="rounded p-1 text-muted-foreground hover:bg-accent disabled:opacity-30" disabled={i === editParams.length - 1} onclick={() => moveParam(i, 1)} aria-label="Move down">
                        <ChevronDown class="h-3.5 w-3.5" />
                      </button>
                      <button type="button" class="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive" onclick={() => removeParam(i)} aria-label="Remove">
                        <X class="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </li>
                {/each}
              </ul>
            {/if}
            <select
              value=""
              onchange={(e: Event) => { const t = e.currentTarget as HTMLSelectElement; addParam(t.value); t.value = ''; }}
              class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              disabled={addable.length === 0}
            >
              <option value="">+ Add parameter…</option>
              {#each addable as d (d.id)}
                <option value={d.slug}>{d.display_name} ({d.slug})</option>
              {/each}
            </select>
          </div>
        </div>

        <div class="flex justify-end gap-2 pt-5">
          <Button variant="outline" type="button" onclick={() => (editOpen = false)}>Cancel</Button>
          <Button type="button" onclick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

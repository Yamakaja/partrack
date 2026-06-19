<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import Dialog from '$lib/components/ui/dialog/Dialog.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import { listCategories, getCategoryParameterSchema } from '$lib/api/categories.js';
  import { batchCreateGenericParts, type BatchGenericResult, type BatchCreateGenericBody } from '$lib/api/parts.js';
  import { ApiError } from '$lib/api/client.js';

  let {
    open = $bindable(false),
    teamId,
    onCreated,
  }: {
    open?: boolean;
    teamId: string;
    onCreated?: () => void;
  } = $props();

  const SERIES = ['E3', 'E6', 'E12', 'E24', 'E48', 'E96', 'E192'];

  // ── config state ────────────────────────────────────────────────────────────
  let categoryId = $state<number | null>(null);
  let pkg = $state('');
  let mode = $state<'eseries' | 'list'>('eseries');
  let series = $state('E24');
  let lower = $state('');
  let upper = $state('');
  let listText = $state('');
  // Fixed secondary parameters, keyed by slug.
  let secondary = $state<Record<string, string>>({});

  // ── preview / submit state ────────────────────────────────────────────────────
  let preview = $state<BatchGenericResult | null>(null);
  let busy = $state(false);
  let error = $state('');

  // Categories the user may target: those configured with a generic_code.
  let categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: () => listCategories(),
    staleTime: 10 * 60_000,
  }));
  let categories = $derived(categoriesQuery.data ?? []);
  let genericIds = $derived(new Set(categories.filter((c) => c.generic_code != null).map((c) => c.id)));

  // Per-category schema: the magnitude unit, known packages, and secondary params.
  let schemaQuery = createQuery(() => ({
    queryKey: ['category-param-schema', teamId, categoryId],
    queryFn: () => getCategoryParameterSchema(teamId, categoryId!),
    enabled: categoryId != null,
    staleTime: 5 * 60_000,
  }));
  let schema = $derived(schemaQuery.data ?? null);
  let valueParamSlug = $derived(schema?.value_param ?? '');
  let valueParam = $derived(schema?.params.find((p) => p.slug === valueParamSlug) ?? null);
  let valueUnit = $derived(valueParam?.unit_symbol ?? '');
  let packageOptions = $derived(schema?.params.find((p) => p.slug === 'package')?.options ?? []);
  // Secondary params are everything curated except the magnitude and the package.
  let secondaryParams = $derived(
    (schema?.params ?? []).filter((p) => p.slug !== valueParamSlug && p.slug !== 'package'),
  );

  // Any config change invalidates a prior preview — force a re-plan before committing.
  function dirty() {
    preview = null;
    error = '';
  }
  function setCategory(id: number | null) {
    categoryId = id;
    secondary = {};
    dirty();
  }

  function buildBody(dryRun: boolean): BatchCreateGenericBody | null {
    if (categoryId == null) return null;
    const params = Object.entries(secondary)
      .map(([slug, value]) => ({ slug, value: value.trim() }))
      .filter((p) => p.value !== '');
    const values: BatchCreateGenericBody['values'] =
      mode === 'eseries'
        ? { mode: 'eseries', series, lower: lower.trim(), upper: upper.trim() }
        : { mode: 'list', list: listText.split(/[\n,;]+/).map((s) => s.trim()).filter(Boolean) };
    return {
      team_id: teamId,
      category_id: categoryId,
      package: pkg.trim() || undefined,
      values,
      params: params.length ? params : undefined,
      dry_run: dryRun,
    };
  }

  let canPreview = $derived(
    categoryId != null &&
      (mode === 'eseries' ? lower.trim() !== '' && upper.trim() !== '' : listText.trim() !== ''),
  );

  async function run(dryRun: boolean) {
    const body = buildBody(dryRun);
    if (!body) return;
    busy = true;
    error = '';
    try {
      const res = await batchCreateGenericParts(body);
      if (dryRun) {
        preview = res;
      } else {
        preview = res;
        onCreated?.();
      }
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Request failed';
    } finally {
      busy = false;
    }
  }

  // After a real run the response has `created`; that marks the committed state.
  let committed = $derived(preview != null && !preview.dry_run);

  function reset() {
    preview = null;
    error = '';
    pkg = '';
    lower = '';
    upper = '';
    listText = '';
    secondary = {};
  }

  $effect(() => {
    if (!open) {
      // Defer so the close animation isn't disrupted.
      categoryId = null;
      reset();
    }
  });
</script>

<Dialog bind:open title="Batch create generic parts" class="max-w-2xl">
  <div class="max-h-[70vh] space-y-4 overflow-auto pr-1">
    <!-- Category -->
    <div class="space-y-1.5">
      <Label>Category</Label>
      <CategoryPicker
        value={categoryId}
        {categories}
        availableIds={genericIds}
        onSelect={setCategory}
        placeholder="Select a generic-capable category…"
      />
      {#if categoryId != null && schema && !valueParamSlug}
        <p class="text-xs text-amber-600 dark:text-amber-400">This category is not parametric.</p>
      {/if}
    </div>

    {#if categoryId != null && valueParamSlug}
      <!-- Package -->
      <div class="space-y-1.5">
        <Label for="batch-pkg">Package</Label>
        <Input
          id="batch-pkg"
          value={pkg}
          oninput={(e: Event) => { pkg = (e.currentTarget as HTMLInputElement).value; dirty(); }}
          list="batch-pkg-options"
          placeholder="e.g. 0402"
          class="h-9"
        />
        {#if packageOptions.length}
          <datalist id="batch-pkg-options">
            {#each packageOptions as opt}<option value={opt}></option>{/each}
          </datalist>
          <p class="text-[11px] text-muted-foreground">Known in this category: {packageOptions.join(', ')}</p>
        {/if}
      </div>

      <!-- Values -->
      <div class="space-y-2">
        <div class="flex items-center justify-between">
          <Label>Values{valueParam ? ` (${valueParam.display_name}${valueUnit ? `, ${valueUnit}` : ''})` : ''}</Label>
          <div class="flex gap-1 rounded-md border border-border p-0.5 text-xs">
            <button
              type="button"
              class="rounded px-2 py-0.5 {mode === 'eseries' ? 'bg-accent font-medium' : 'text-muted-foreground'}"
              onclick={() => { mode = 'eseries'; dirty(); }}>E-series</button>
            <button
              type="button"
              class="rounded px-2 py-0.5 {mode === 'list' ? 'bg-accent font-medium' : 'text-muted-foreground'}"
              onclick={() => { mode = 'list'; dirty(); }}>Custom list</button>
          </div>
        </div>

        {#if mode === 'eseries'}
          <div class="flex flex-wrap items-end gap-2">
            <div class="space-y-1">
              <span class="text-[11px] text-muted-foreground">Series</span>
              <select
                class="block h-9 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                value={series}
                onchange={(e: Event) => { series = (e.currentTarget as HTMLSelectElement).value; dirty(); }}
              >
                {#each SERIES as s}<option value={s}>{s}</option>{/each}
              </select>
            </div>
            <div class="space-y-1">
              <span class="text-[11px] text-muted-foreground">From</span>
              <Input
                value={lower}
                oninput={(e: Event) => { lower = (e.currentTarget as HTMLInputElement).value; dirty(); }}
                placeholder="10"
                class="h-9 w-28"
              />
            </div>
            <div class="space-y-1">
              <span class="text-[11px] text-muted-foreground">To</span>
              <Input
                value={upper}
                oninput={(e: Event) => { upper = (e.currentTarget as HTMLInputElement).value; dirty(); }}
                placeholder="1M"
                class="h-9 w-28"
              />
            </div>
          </div>
          <p class="text-[11px] text-muted-foreground">
            SI prefixes and RKM accepted (e.g. 4k7, 100n). Bounds are inclusive.
          </p>
        {:else}
          <textarea
            class="min-h-20 w-full rounded-md border border-input bg-background p-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            value={listText}
            oninput={(e: Event) => { listText = (e.currentTarget as HTMLTextAreaElement).value; dirty(); }}
            placeholder="One value per line, or comma-separated (e.g. 10k, 4k7, 100k)"
          ></textarea>
        {/if}
      </div>

      <!-- Fixed secondary parameters (optional) -->
      {#if secondaryParams.length}
        <div class="space-y-2">
          <Label>Fixed parameters <span class="font-normal text-muted-foreground">(optional, applied to all)</span></Label>
          <div class="grid grid-cols-2 gap-2">
            {#each secondaryParams as p}
              <label class="space-y-1">
                <span class="text-[11px] text-muted-foreground">{p.display_name}{p.unit_symbol ? ` (${p.unit_symbol})` : ''}</span>
                <Input
                  value={secondary[p.slug] ?? ''}
                  oninput={(e: Event) => { secondary = { ...secondary, [p.slug]: (e.currentTarget as HTMLInputElement).value }; dirty(); }}
                  list={p.options.length ? `sec-${p.slug}` : undefined}
                  placeholder="—"
                  class="h-9"
                />
                {#if p.options.length}
                  <datalist id={`sec-${p.slug}`}>
                    {#each p.options as opt}<option value={opt}></option>{/each}
                  </datalist>
                {/if}
              </label>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Preview / result -->
      {#if preview}
        <div class="rounded-md border border-border">
          <div class="flex items-center gap-2 border-b border-border px-3 py-2 text-sm">
            {#if committed}
              <span class="font-medium text-green-700 dark:text-green-400">Created {preview.created} part{preview.created === 1 ? '' : 's'}</span>
              <span class="text-muted-foreground">· {preview.existing} already existed · {preview.total} total</span>
            {:else}
              <span class="font-medium">{preview.to_create} to create</span>
              <span class="text-muted-foreground">· {preview.existing} already exist · {preview.total} total</span>
            {/if}
          </div>
          <div class="max-h-56 divide-y divide-border overflow-auto">
            {#each preview.items as it}
              <div class="flex items-center gap-2 px-3 py-1 text-xs">
                <span class="font-mono">{it.value}</span>
                <span class="truncate text-muted-foreground">{it.mpn}</span>
                <span class="ml-auto shrink-0">
                  {#if it.existing}
                    <Badge variant="secondary">exists</Badge>
                  {:else if committed}
                    <Badge variant="success">created</Badge>
                  {:else}
                    <Badge>new</Badge>
                  {/if}
                </span>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if error}
        <p class="text-sm text-destructive">{error}</p>
      {/if}
    {/if}
  </div>

  <!-- Footer -->
  <div class="mt-4 flex justify-end gap-2">
    {#if committed}
      <Button variant="outline" onclick={() => { reset(); }}>Create more</Button>
      <Button onclick={() => (open = false)}>Done</Button>
    {:else}
      <Button variant="outline" onclick={() => (open = false)}>Cancel</Button>
      {#if preview}
        <Button onclick={() => run(false)} disabled={busy || preview.to_create === 0}>
          {busy ? 'Creating…' : `Create ${preview.to_create} part${preview.to_create === 1 ? '' : 's'}`}
        </Button>
      {:else}
        <Button onclick={() => run(true)} disabled={busy || !canPreview}>
          {busy ? 'Planning…' : 'Preview'}
        </Button>
      {/if}
    {/if}
  </div>
</Dialog>

<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import CategoryPicker from '$lib/components/CategoryPicker.svelte';
  import { getCategoryParameterSchema, listCategories, type CategoryParamSchema } from '$lib/api/categories.js';
  import { type Interpretation, type InterpretRoute } from '$lib/api/revisions.js';
  import { Plus, Trash2 } from 'lucide-svelte';

  // The interpretation is the editable *result*; source fields are read-only context.
  let {
    interpretation = $bindable(),
    source,
    teamId,
    readonly = false,
    onchange,
    onReparse,
  }: {
    interpretation: Interpretation;
    source: {
      reference_designators?: string;
      value?: string | null;
      footprint?: string | null;
      mpn?: string | null;
      sku?: string | null;
      manufacturer?: string | null;
    };
    teamId: string;
    readonly?: boolean;
    /** Called after any interpretation edit so the parent can mark the row dirty. */
    onchange?: () => void;
    /** When provided, the source Value/Footprint become editable and trigger a
        re-parse via this callback (import step only). */
    onReparse?: (value: string, footprint: string) => void;
  } = $props();

  // Categories are global; the shared query key dedupes across many editor rows.
  let categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: () => listCategories(),
    staleTime: 10 * 60_000,
  }));
  let categories = $derived(categoriesQuery.data ?? []);

  const ROUTES: { value: InterpretRoute; label: string }[] = [
    { value: 'parametric', label: 'Generic (parametric)' },
    { value: 'identity', label: 'Specific part' },
    { value: 'ignore', label: 'Ignore' },
    { value: 'unknown', label: 'Needs review' },
  ];

  // ── category slug ↔ id ────────────────────────────────────────────────────────
  let categoryById = $derived(new Map(categories.map((c) => [c.id, c])));
  let idBySlug = $derived(new Map(categories.map((c) => [c.slug, c.id])));
  let categoryId = $derived(idBySlug.get(interpretation.category) ?? null);
  // Categories the parser can target generically: a value magnitude or named params.
  let availableIds = $derived(
    new Set(categories.filter((c) => c.value_param != null || c.primary_params.length > 0).map((c) => c.id)),
  );

  // ── per-category parameter schema (labels, value types, enum vocab) ───────────
  let schemaQuery = createQuery(() => ({
    queryKey: ['category-param-schema', teamId, categoryId],
    queryFn: () => getCategoryParameterSchema(teamId, categoryId!),
    enabled: categoryId != null,
    staleTime: 5 * 60_000,
  }));
  let schemaBySlug = $derived(
    new Map<string, CategoryParamSchema>((schemaQuery.data?.params ?? []).map((p) => [p.slug, p])),
  );

  function label(slug: string): string {
    return schemaBySlug.get(slug)?.display_name ?? slug;
  }
  function unit(slug: string): string {
    return schemaBySlug.get(slug)?.unit_symbol ?? '';
  }
  function options(slug: string): string[] {
    return schemaBySlug.get(slug)?.options ?? [];
  }
  // Schema params not already on the line — the add-parameter menu.
  let addable = $derived(
    (schemaQuery.data?.params ?? []).filter((p) => !(interpretation.params ?? []).some((x) => x.slug === p.slug)),
  );

  // ── edits ─────────────────────────────────────────────────────────────────────
  function touch() {
    onchange?.();
  }
  function setRoute(route: InterpretRoute) {
    interpretation = { ...interpretation, route };
    touch();
  }
  function setCategory(id: number | null) {
    interpretation = { ...interpretation, category: id == null ? '' : (categoryById.get(id)?.slug ?? '') };
    touch();
  }
  function setParamValue(i: number, raw: string) {
    const params = [...(interpretation.params ?? [])];
    // value_si is derived server-side; null it so a stale magnitude never lingers.
    params[i] = { ...params[i], raw: raw.trim(), value_si: null };
    interpretation = { ...interpretation, params };
    touch();
  }
  function setParamSlug(i: number, slug: string) {
    const params = [...(interpretation.params ?? [])];
    params[i] = { ...params[i], slug, value_si: null };
    interpretation = { ...interpretation, params };
    touch();
  }
  function removeParam(i: number) {
    const params = [...(interpretation.params ?? [])];
    params.splice(i, 1);
    interpretation = { ...interpretation, params };
    touch();
  }
  function addParam(slug: string) {
    if (!slug) return;
    const params = [...(interpretation.params ?? []), { slug, raw: '', value_si: null }];
    interpretation = { ...interpretation, params };
    touch();
  }
  function setIdentity(field: 'mpn' | 'manufacturer' | 'sku', v: string) {
    interpretation = { ...interpretation, [field]: v.trim() };
    touch();
  }

  // ── source editing (import step only) ─────────────────────────────────────────
  let editValue = $state('');
  let editFootprint = $state('');
  $effect(() => {
    editValue = source.value ?? '';
    editFootprint = source.footprint ?? '';
  });
</script>

<div class="flex flex-col gap-3 lg:flex-row lg:items-start">
  <!-- Zone 1: source (from the BOM) — read-only context, optionally re-parseable -->
  <div class="min-w-0 flex-1 space-y-2">
    <div class="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">From BOM</div>
    {#if onReparse && !readonly}
      <div class="flex flex-wrap items-center gap-2">
        <label class="flex items-center gap-1 text-xs text-muted-foreground">
          Value
          <Input
            value={editValue}
            oninput={(e: Event) => (editValue = (e.currentTarget as HTMLInputElement).value)}
            onchange={() => onReparse?.(editValue, editFootprint)}
            class="h-7 w-32 text-xs"
            placeholder="—"
          />
        </label>
        <label class="flex items-center gap-1 text-xs text-muted-foreground">
          Footprint
          <Input
            value={editFootprint}
            oninput={(e: Event) => (editFootprint = (e.currentTarget as HTMLInputElement).value)}
            onchange={() => onReparse?.(editValue, editFootprint)}
            class="h-7 w-44 text-xs"
            placeholder="—"
          />
        </label>
      </div>
    {:else}
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
        {#if source.value}<dt class="text-muted-foreground">Value</dt><dd class="font-mono">{source.value}</dd>{/if}
        {#if source.footprint}<dt class="text-muted-foreground">Footprint</dt><dd class="font-mono">{source.footprint}</dd>{/if}
      </dl>
    {/if}
    {#if source.mpn || source.sku}
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
        {#if source.manufacturer}<dt class="text-muted-foreground">Mfr</dt><dd>{source.manufacturer}</dd>{/if}
        {#if source.mpn}<dt class="text-muted-foreground">MPN</dt><dd class="font-mono">{source.mpn}</dd>{/if}
        {#if source.sku}<dt class="text-muted-foreground">SKU</dt><dd class="font-mono">{source.sku}</dd>{/if}
      </dl>
    {/if}
  </div>

  <!-- Zone 2: interpretation (the result) — editable -->
  <div class="min-w-0 flex-1 space-y-2">
    <div class="flex items-center gap-2">
      <span class="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Interpretation</span>
      {#if !readonly}
        <select
          class="ml-auto h-7 rounded-md border border-input bg-background px-2 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          value={interpretation.route}
          onchange={(e: Event) => setRoute((e.currentTarget as HTMLSelectElement).value as InterpretRoute)}
        >
          {#each ROUTES as r}<option value={r.value}>{r.label}</option>{/each}
        </select>
      {/if}
    </div>

    {#if interpretation.route === 'parametric'}
      <!-- Category -->
      <div class="flex items-center gap-2 text-xs">
        <span class="text-muted-foreground">Category</span>
        {#if readonly}
          <span>{categoryById.get(categoryId ?? -1)?.display_name || interpretation.category || '—'}</span>
        {:else}
          <CategoryPicker
            value={categoryId}
            {categories}
            {availableIds}
            onSelect={setCategory}
            placeholder="Select category…"
          />
        {/if}
      </div>

      <!-- Parameter table (package is just a row) -->
      <div class="rounded-md border border-border">
        <div class="divide-y divide-border">
          {#each interpretation.params ?? [] as p, i}
            <div class="flex items-center gap-1.5 px-2 py-1 text-xs">
              {#if readonly}
                <span class="flex-1 truncate text-muted-foreground">{label(p.slug)}</span>
                <span class="font-mono">{p.raw}{unit(p.slug) ? ' ' + unit(p.slug) : ''}</span>
              {:else}
                <select
                  class="h-7 w-40 shrink-0 rounded-md border border-input bg-background px-1.5 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  value={p.slug}
                  onchange={(e: Event) => setParamSlug(i, (e.currentTarget as HTMLSelectElement).value)}
                >
                  {#if !schemaBySlug.has(p.slug)}<option value={p.slug}>{p.slug}</option>{/if}
                  {#each schemaQuery.data?.params ?? [] as sp}<option value={sp.slug}>{sp.display_name}</option>{/each}
                </select>
                <Input
                  value={p.raw}
                  onchange={(e: Event) => setParamValue(i, (e.currentTarget as HTMLInputElement).value)}
                  class="h-7 min-w-0 flex-1 text-xs"
                  placeholder="value"
                  list={options(p.slug).length ? `opts-${i}-${p.slug}` : undefined}
                />
                {#if options(p.slug).length}
                  <datalist id={`opts-${i}-${p.slug}`}>
                    {#each options(p.slug) as opt}<option value={opt}></option>{/each}
                  </datalist>
                {/if}
                {#if unit(p.slug)}<span class="shrink-0 text-muted-foreground">{unit(p.slug)}</span>{/if}
                <button
                  type="button"
                  onclick={() => removeParam(i)}
                  title="Remove parameter"
                  class="shrink-0 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                >
                  <Trash2 class="h-3.5 w-3.5" />
                </button>
              {/if}
            </div>
          {/each}
          {#if (interpretation.params?.length ?? 0) === 0}
            <div class="px-2 py-1.5 text-xs italic text-muted-foreground">No parameters</div>
          {/if}
        </div>
        {#if !readonly && addable.length > 0}
          <div class="flex flex-wrap items-center gap-1 border-t border-border px-2 py-1">
            <Plus class="h-3 w-3 text-muted-foreground" />
            {#each addable as a}
              <button
                type="button"
                onclick={() => addParam(a.slug)}
                class="rounded border border-border px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-accent"
              >{a.display_name}</button>
            {/each}
          </div>
        {/if}
      </div>
    {:else if interpretation.route === 'identity'}
      <!-- Discovered identity: editable when not supplied by a BOM column -->
      <dl class="grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1 text-xs">
        {#each [['manufacturer', 'Mfr'], ['mpn', 'MPN'], ['sku', 'SKU']] as [field, lbl]}
          {@const fromColumn = !!(source as any)[field]}
          <dt class="text-muted-foreground">{lbl}</dt>
          <dd>
            {#if fromColumn}
              <span class="font-mono">{(source as any)[field]}</span>
              <span class="ml-1 text-[10px] text-muted-foreground">(from BOM)</span>
            {:else if readonly}
              <span class="font-mono">{(interpretation as any)[field] || '—'}</span>
            {:else}
              <Input
                value={(interpretation as any)[field] ?? ''}
                onchange={(e: Event) => setIdentity(field as 'mpn' | 'manufacturer' | 'sku', (e.currentTarget as HTMLInputElement).value)}
                class="h-7 w-full text-xs"
                placeholder="—"
              />
            {/if}
          </dd>
        {/each}
      </dl>
    {:else if interpretation.route === 'ignore'}
      <p class="text-xs text-muted-foreground">Excluded from stock checks.</p>
    {:else}
      <p class="text-xs text-warning-foreground">Couldn't interpret — pick a route, or resolve it after import.</p>
    {/if}
  </div>
</div>

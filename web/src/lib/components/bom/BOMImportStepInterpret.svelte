<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { interpretPreview, type Interpretation, type InterpretRoute } from '$lib/api/revisions.js';
  import type { BOMImportItem } from './bom-import-utils.js';

  let {
    items = $bindable(),
    interpretations = $bindable(),
    edited = $bindable(),
    teamId,
    loading,
    onback,
    onnext,
    oncancel,
  }: {
    items: BOMImportItem[];
    interpretations: Interpretation[];
    edited: Record<number, boolean>;
    teamId: string;
    loading: boolean;
    onback: () => void;
    onnext: () => void;
    oncancel: () => void;
  } = $props();

  const ROUTES: { value: InterpretRoute; label: string }[] = [
    { value: 'parametric', label: 'Generic (parametric)' },
    { value: 'identity', label: 'Specific part' },
    { value: 'ignore', label: 'Ignore' },
    { value: 'unknown', label: 'Needs review' },
  ];

  const ROUTE_BADGE: Record<InterpretRoute, { label: string; variant: 'success' | 'default' | 'warning' | 'secondary' }> = {
    parametric: { label: 'Generic', variant: 'default' },
    identity: { label: 'Specific', variant: 'success' },
    ignore: { label: 'Ignored', variant: 'secondary' },
    unknown: { label: 'Review', variant: 'warning' },
  };

  function needsAttention(it: Interpretation): boolean {
    if (it.reused) return false; // a remembered correction was already confirmed
    return it.route === 'unknown' || (it.flags?.length ?? 0) > 0 || (it.confidence ?? 1) < 0.7;
  }

  // Attention score: higher sorts to the top. Problematic/low-confidence rows
  // first; confident rows below for a quick scroll-through (they can still be wrong).
  // Remembered corrections sink to the bottom — they need the least attention.
  function score(it: Interpretation): number {
    if (it.reused) return -1;
    let s = 0;
    if (it.route === 'unknown') s += 100;
    s += (it.flags?.length ?? 0) * 50;
    s += Math.round((1 - (it.confidence ?? 1)) * 40);
    return s;
  }

  let order = $derived(
    interpretations
      .map((it, i) => ({ i, s: score(it) }))
      .sort((a, b) => b.s - a.s || a.i - b.i)
      .map((o) => o.i),
  );

  let summary = $derived.by(() => {
    const c: Record<InterpretRoute, number> = { parametric: 0, identity: 0, ignore: 0, unknown: 0 };
    for (const it of interpretations) c[it.route]++;
    return c;
  });

  let attentionCount = $derived(interpretations.filter(needsAttention).length);
  let reusedCount = $derived(interpretations.filter((it) => it.reused).length);

  function setRoute(i: number, route: InterpretRoute) {
    interpretations[i] = { ...interpretations[i], route };
    edited[i] = true;
  }

  function setPackage(i: number, pkg: string) {
    interpretations[i] = { ...interpretations[i], package: pkg.trim() };
    edited[i] = true;
  }

  let reparsing = $state<Record<number, boolean>>({});

  // Re-run the parser for one row after its value/footprint is edited, so the
  // re-extracted parameters (and SI values) stay correct rather than going stale.
  async function reparse(i: number) {
    reparsing[i] = true;
    try {
      const res = await interpretPreview(
        [
          {
            refdes: items[i].reference_designators,
            footprint: items[i].footprint,
            value: items[i].value,
            mpn: items[i].mpn,
            sku: items[i].sku,
          },
        ],
        teamId,
      );
      if (res.items[0]) {
        interpretations[i] = res.items[0];
        edited[i] = true;
      }
    } finally {
      reparsing[i] = false;
    }
  }

  function fmtParam(p: { slug: string; raw: string }): string {
    return `${p.slug}: ${p.raw}`;
  }
</script>

<div class="min-h-0 flex-1 overflow-auto px-6 py-4 space-y-3">
  {#if loading}
    <div class="py-12 text-center text-sm text-muted-foreground">Interpreting BOM rows…</div>
  {:else}
    <!-- Summary -->
    <div class="flex flex-wrap items-center gap-2 text-xs">
      <span class="text-muted-foreground">How each row was read:</span>
      <Badge variant="default">{summary.parametric} generic</Badge>
      <Badge variant="success">{summary.identity} specific</Badge>
      <Badge variant="secondary">{summary.ignore} ignored</Badge>
      {#if summary.unknown > 0}<Badge variant="warning">{summary.unknown} needs review</Badge>{/if}
      {#if reusedCount > 0}<span title="Reused from your team's earlier corrections"><Badge variant="outline">{reusedCount} remembered</Badge></span>{/if}
      <span class="ml-auto text-muted-foreground">
        {#if attentionCount > 0}
          {attentionCount} row{attentionCount === 1 ? '' : 's'} need attention (sorted to top)
        {:else}
          Looks clean — scroll to spot-check
        {/if}
      </span>
    </div>

    <!-- Dense, attention-first list -->
    <div class="divide-y divide-border rounded-md border border-border">
      {#each order as i (i)}
        {@const it = interpretations[i]}
        <div class="flex flex-col gap-2 px-3 py-2 text-sm {needsAttention(it) ? 'bg-warning/5' : ''}">
          <div class="flex items-center gap-2">
            <span class="font-medium">{items[i].reference_designators}</span>
            <Badge variant={ROUTE_BADGE[it.route].variant}>{ROUTE_BADGE[it.route].label}</Badge>
            {#if it.reused}<span title="Reused from your team's earlier corrections"><Badge variant="outline">remembered</Badge></span>{/if}
            {#if edited[i]}<span class="text-[10px] uppercase tracking-wide text-primary">edited</span>{/if}
            {#each it.flags ?? [] as flag}
              <span class="text-[10px] rounded bg-warning/20 px-1.5 py-0.5 text-warning-foreground">{flag}</span>
            {/each}
            <select
              class="ml-auto h-7 rounded-md border border-input bg-background px-2 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              value={it.route}
              onchange={(e: Event) => setRoute(i, (e.currentTarget as HTMLSelectElement).value as InterpretRoute)}
            >
              {#each ROUTES as r}
                <option value={r.value}>{r.label}</option>
              {/each}
            </select>
          </div>

          <!-- Editable raw inputs: editing re-parses the row -->
          <div class="flex flex-wrap items-center gap-2">
            <label class="flex items-center gap-1 text-xs text-muted-foreground">
              Value
              <Input
                value={items[i].value ?? ''}
                oninput={(e: Event) => (items[i].value = (e.currentTarget as HTMLInputElement).value)}
                onchange={() => reparse(i)}
                class="h-7 w-32 text-xs"
                placeholder="—"
              />
            </label>
            <label class="flex items-center gap-1 text-xs text-muted-foreground">
              Footprint
              <Input
                value={items[i].footprint ?? ''}
                oninput={(e: Event) => (items[i].footprint = (e.currentTarget as HTMLInputElement).value)}
                onchange={() => reparse(i)}
                class="h-7 w-44 text-xs"
                placeholder="—"
              />
            </label>
            {#if reparsing[i]}<span class="text-xs text-muted-foreground">re-parsing…</span>{/if}
          </div>

          <!-- Interpretation summary -->
          {#if it.route === 'parametric'}
            <div class="flex flex-wrap items-center gap-1.5 text-xs">
              {#if it.category}<span class="text-muted-foreground">{it.category}</span>{/if}
              <label class="flex items-center gap-1">
                <span class="text-muted-foreground">pkg</span>
                <Input
                  value={it.package}
                  onchange={(e: Event) => setPackage(i, (e.currentTarget as HTMLInputElement).value)}
                  class="h-6 w-20 text-xs"
                  placeholder="—"
                />
              </label>
              {#each it.params as p}
                <Badge variant="outline">{fmtParam(p)}</Badge>
              {/each}
              {#if it.params.length === 0}
                <span class="text-muted-foreground italic">no parameters extracted</span>
              {/if}
            </div>
          {:else if it.route === 'identity'}
            <div class="text-xs text-muted-foreground">Resolved by MPN/SKU during import.</div>
          {:else if it.route === 'ignore'}
            <div class="text-xs text-muted-foreground">Excluded from stock checks.</div>
          {:else}
            <div class="text-xs text-warning-foreground">
              Couldn't interpret — fix the value/footprint above, pick a route, or resolve it after import.
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- Footer -->
<div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
  <Button variant="outline" onclick={onback}>← Back</Button>
  <div class="flex gap-2">
    <Button variant="outline" onclick={oncancel}>Cancel</Button>
    <Button onclick={onnext} disabled={loading}>Continue →</Button>
  </div>
</div>

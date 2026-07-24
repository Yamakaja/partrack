<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { LinkPreview } from 'bits-ui';
  import { Check, Cpu, ListPlus } from 'lucide-svelte';
  import { getPart } from '$lib/api/parts.js';
  import { listCategories } from '$lib/api/categories.js';
  import { listCollections, createCollection, addCollectionItem } from '$lib/api/collections.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { formatSI } from '$lib/utils/si.js';
  import { coordLabel } from '$lib/components/mobile-scanner/locationUtils.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { listPanelStore, setActiveList } from '$lib/stores/listPanel.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import type { Part, Parameter, StorageLocationRef } from '$lib/types/parts.js';
  import type { Category } from '$lib/types/categories.js';

  // The floating content of a part reference. Must be rendered inside a LinkPreview.Root;
  // `open` mirrors the root's state so the part is only fetched once the card opens.
  let {
    id,
    teamId,
    open,
  }: {
    id: string;
    teamId?: string;
    open: boolean;
  } = $props();

  // Every object card takes the same {id, teamId, open} contract; the part
  // internals below predate it and still read `partId`.
  const partId = $derived(id);

  const partQuery = createQuery(() => ({
    queryKey: ['part', partId, teamId],
    queryFn: () => getPart(partId, teamId),
    enabled: open,
    staleTime: 60_000,
  }));

  const categoriesQuery = createQuery(() => ({
    queryKey: ['categories'],
    queryFn: listCategories,
    staleTime: Infinity,
    enabled: open,
  }));

  // "Add to list": the hover card is mounted on every part reference in the
  // app, so this one affordance makes any part anywhere addable to the active
  // list. Bare membership (no quantity) — counts are set on the list itself.
  const qc = useQueryClient();
  const canUseLists = $derived(
    !!teamId &&
      activeTeamStore.current?.teamId === teamId &&
      (activeTeamStore.current?.role ?? 'viewer') !== 'viewer',
  );
  const collectionsQuery = createQuery(() => ({
    queryKey: ['collections', teamId],
    queryFn: () => listCollections(teamId!),
    enabled: open && canUseLists,
    staleTime: 30_000,
  }));
  const activeList = $derived(
    collectionsQuery.data?.collections.find((c) => c.id === listPanelStore.activeListId) ?? null,
  );

  let addBusy = $state(false);
  let justAdded = $state(false);

  async function addToList(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    if (!teamId || addBusy) return;
    addBusy = true;
    try {
      let listId = listPanelStore.activeListId;
      if (!listId) {
        const created = await createCollection(teamId);
        setActiveList(created.id);
        listId = created.id;
      }
      await addCollectionItem(teamId, listId, { part_id: partId });
      qc.invalidateQueries({ queryKey: ['collections', teamId] });
      qc.invalidateQueries({ queryKey: ['collection', teamId, listId] });
      justAdded = true;
      setTimeout(() => (justAdded = false), 1500);
    } catch (err) {
      pushToast('error', err instanceof Error ? err.message : 'Could not add to list');
    } finally {
      addBusy = false;
    }
  }

  interface Spec {
    slug: string;
    label: string;
    value: string;
  }

  // One primary parameter, resolved with the app's precedence:
  // user-specified wins, then the preferred distributor source, then any source.
  function resolveParam(part: Part, slug: string): Spec | null {
    const bySlug = (list: Parameter[]) => list.filter((p) => p.parameter_slug === slug);
    const user = bySlug(part.parameters?.user_specified ?? []);
    let chosen = user[0];
    if (!chosen) {
      const dist = bySlug(part.parameters?.distributor_sourced ?? []);
      chosen =
        dist.find(
          (p) =>
            p.source_distributor_id != null &&
            p.source_distributor_id === part.preferred_parameter_source_id,
        ) ?? dist[0];
    }
    if (!chosen) return null;

    const label = chosen.parameter_display_name ?? chosen.name;
    const sym = chosen.parameter_unit_symbol;
    const fmt = (n: number) => (sym ? formatSI(n, sym) : String(parseFloat(n.toPrecision(4))));
    let value: string;
    if (chosen.numeric_value != null) {
      value =
        chosen.numeric_value_max != null && chosen.numeric_value_max !== chosen.numeric_value
          ? `${fmt(chosen.numeric_value)}–${fmt(chosen.numeric_value_max)}`
          : fmt(chosen.numeric_value);
    } else {
      value = chosen.unit ? `${chosen.value} ${chosen.unit}` : chosen.value;
    }
    return { slug, label, value };
  }

  // Primary parameters: value_param first, then the curated primary_params
  // order, capped at 4. Categories are hierarchical and params are frequently
  // curated on a parent ("Capacitors") while the part sits on a leaf
  // ("Ceramic Capacitors") that adds none of its own — so walk the chain from
  // the part's category up to the root, inheriting slugs (nearest category
  // first). No curated ancestor anywhere => no block at all.
  function resolveSpecs(part: Part, categories: Category[]): Spec[] {
    if (part.category_id == null) return [];
    const byId = new Map(categories.map((c) => [c.id, c]));
    const slugs: string[] = [];
    const seen = new Set<number>(); // guard against a malformed parent cycle
    let cat = byId.get(part.category_id);
    while (cat && !seen.has(cat.id)) {
      seen.add(cat.id);
      if (cat.value_param && !slugs.includes(cat.value_param)) slugs.push(cat.value_param);
      for (const s of cat.primary_params) if (!slugs.includes(s)) slugs.push(s);
      cat = cat.parent_id != null ? byId.get(cat.parent_id) : undefined;
    }

    const specs: Spec[] = [];
    for (const slug of slugs) {
      if (specs.length >= 4) break;
      const spec = resolveParam(part, slug);
      if (spec) specs.push(spec);
    }
    if (specs.length > 0 && specs.length < 4 && part.package && !specs.some((s) => s.slug === 'package')) {
      specs.push({ slug: 'package', label: 'Package', value: part.package });
    }
    return specs;
  }

  function locationText(loc: StorageLocationRef): string {
    let s = [loc.ancestor_path, loc.label].filter(Boolean).join(' › ');
    if (loc.coordinate?.length && loc.grid_config) {
      const c = coordLabel(loc.coordinate, loc.grid_config);
      if (c) s += ` [${c}]`;
    }
    return s;
  }

  function stockDot(qty: number, min: number | null): string {
    if (min == null) return qty > 0 ? 'bg-success' : 'bg-danger';
    if (qty >= min) return 'bg-success';
    if (qty > 0) return 'bg-warning';
    return 'bg-danger';
  }
</script>

<LinkPreview.Portal>
  <LinkPreview.Content
    side="bottom"
    align="start"
    sideOffset={8}
    class="z-50 w-[380px] rounded-lg border border-border bg-popover text-popover-foreground shadow-lg outline-none
           data-[state=open]:animate-in data-[state=closed]:animate-out
           data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
  >
    {#if partQuery.isPending}
      <div class="flex gap-3 p-3.5">
        <div class="h-14 w-14 shrink-0 animate-pulse rounded-md bg-muted"></div>
        <div class="flex-1 space-y-2 py-1">
          <div class="h-3.5 w-36 animate-pulse rounded bg-muted"></div>
          <div class="h-3 w-24 animate-pulse rounded bg-muted"></div>
          <div class="h-3 w-full animate-pulse rounded bg-muted"></div>
        </div>
      </div>
    {:else if partQuery.isError}
      <p class="p-3.5 text-xs text-muted-foreground">Couldn't load part details.</p>
    {:else if partQuery.data}
      {@const part = partQuery.data}
      {@const title = part.mpn ?? part.short_description ?? 'Generic part'}
      {@const desc = [part.description, part.short_description].find((d) => d && d !== title) ?? null}
      {@const specs = resolveSpecs(part, categoriesQuery.data ?? [])}
      {@const subtitle = [part.manufacturer, part.category_name].filter(Boolean).join(' · ')}

      <!-- Identity: image tile + MPN / manufacturer / full description -->
      <div class="flex gap-3 p-3.5">
        <div
          class="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border {part.image_url
            ? 'bg-white'
            : 'bg-muted'}"
        >
          {#if part.image_url}
            <img src={part.image_url} alt={title} class="h-full w-full object-contain p-1" />
          {:else}
            <Cpu class="h-6 w-6 text-muted-foreground/50" />
          {/if}
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class={part.mpn ? 'font-mono text-[13px] font-semibold' : 'text-[13px] font-medium italic'}>{title}</span>
            <LifecycleBadge status={part.lifecycle_status} />
            {#if canUseLists}
              <button
                onclick={addToList}
                disabled={addBusy}
                title={justAdded
                  ? 'Added'
                  : activeList
                    ? `Add to “${activeList.name}”`
                    : 'Add to a new list'}
                class="ml-auto shrink-0 rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-50"
              >
                {#if justAdded}
                  <Check class="h-3.5 w-3.5 text-success" />
                {:else}
                  <ListPlus class="h-3.5 w-3.5" />
                {/if}
              </button>
            {/if}
          </div>
          {#if subtitle}
            <p class="truncate text-[11.5px] text-muted-foreground">{subtitle}</p>
          {/if}
          {#if desc}
            <p class="mt-1.5 text-xs leading-relaxed">{desc}</p>
          {/if}
        </div>
      </div>

      <!-- Primary parameters -->
      {#if specs.length > 0}
        <div class="mx-3.5 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border py-2.5">
          {#each specs as spec (spec.slug)}
            <div class="min-w-0">
              <p class="text-[10px] uppercase tracking-wider text-muted-foreground">{spec.label}</p>
              <p class="truncate font-mono text-[11.5px] tabular-nums" title={spec.value}>{spec.value}</p>
            </div>
          {/each}
        </div>
      {/if}

      <!-- Team stock + location -->
      {#if teamId}
        <div class="flex items-center gap-2 border-t border-border px-3.5 py-2 text-[11.5px]">
          {#if part.inventory_entry_id != null}
            {@const qty = part.quantity_on_hand ?? 0}
            <span class="h-[7px] w-[7px] shrink-0 rounded-full {stockDot(qty, part.minimum_stock_quantity)}"></span>
            <span class="shrink-0">
              <span class="font-mono font-semibold tabular-nums">{qty.toLocaleString()}</span> on hand
            </span>
            {#if part.storage_location}
              {@const loc = locationText(part.storage_location)}
              <span class="ml-auto min-w-0 truncate text-muted-foreground" title={loc}>{loc}</span>
            {/if}
          {:else}
            <span class="h-[7px] w-[7px] shrink-0 rounded-full bg-muted-foreground/40"></span>
            <span class="text-muted-foreground">Not tracked in inventory</span>
          {/if}
        </div>
      {/if}

      <!-- Links -->
      {#if part.datasheet_url || part.product_url || teamId}
        <div class="flex items-center gap-3.5 border-t border-border px-3.5 py-2 text-[11.5px] font-medium">
          {#if part.datasheet_url}
            <a href={part.datasheet_url} target="_blank" rel="noopener noreferrer" class="text-info hover:underline">Datasheet ↗</a>
          {/if}
          {#if part.product_url}
            <a href={part.product_url} target="_blank" rel="noopener noreferrer" class="text-info hover:underline">Product ↗</a>
          {/if}
          {#if teamId}
            <a use:linkAction href="/teams/{teamId}/parts/{partId}" class="ml-auto text-muted-foreground hover:text-foreground">
              Open part →
            </a>
          {/if}
        </div>
      {/if}
    {/if}
  </LinkPreview.Content>
</LinkPreview.Portal>

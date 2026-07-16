<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { untrack } from 'svelte';
  import { Dialog } from 'bits-ui';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { ArrowUp, ArrowDown, Trash2, Download, X } from 'lucide-svelte';
  import PartsTable from '$lib/components/parts/PartsTable.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import PartRef from '$lib/components/parts/PartRef.svelte';
  import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
  import type { ColumnSpec } from '$lib/components/table/index.js';
  import type { BOMLineItem } from '$lib/types/projects.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import type { LineItemSubstituteItem } from '$lib/api/revisions.js';
  import {
    listLineItemSubstitutes,
    addSubstitute,
    removeSubstitute,
    reorderSubstitutes,
    importSubstitutes,
    listPreviousRevisionsWithSubstitutes,
  } from '$lib/api/revisions.js';
  import { getPart } from '$lib/api/parts.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { formatSI } from '$lib/utils/si.js';
  import { cn } from '$lib/utils/cn.js';

  let {
    teamId,
    projectId,
    revisionId,
    lineItem,
    open = $bindable(false),
  }: {
    teamId: string;
    projectId: string;
    revisionId: string;
    lineItem: BOMLineItem;
    open?: boolean;
  } = $props();

  const queryClient = useQueryClient();

  const substitutesQuery = createQuery(() => ({
    queryKey: ['revision', revisionId, 'item', lineItem.id, 'substitutes'],
    queryFn: () => listLineItemSubstitutes(teamId, projectId, revisionId, lineItem.id),
    enabled: open,
  }));

  const previousRevisionsQuery = createQuery(() => ({
    queryKey: ['revision', revisionId, 'item', lineItem.id, 'previous-revisions'],
    queryFn: () => listPreviousRevisionsWithSubstitutes(teamId, projectId, revisionId, lineItem.id),
    enabled: open,
  }));

  const primaryPartQuery = createQuery(() => ({
    queryKey: ['part', lineItem.resolved_part_id, teamId],
    queryFn: () => getPart(lineItem.resolved_part_id!, teamId),
    enabled: open && !!lineItem.resolved_part_id,
    staleTime: 5 * 60 * 1000,
  }));

  let selectedImportRevId = $state('');
  let importing = $state(false);
  let removing = $state<Record<string, boolean>>({});
  let reordering = $state(false);
  let partsTableRef = $state<{ setFilter: (k: string, v: Set<string>) => void; setCategoryFilter: (id: number | null) => void } | null>(null);
  let chipModes = $state<Record<string, 'eq' | 'gte' | 'lte' | 'tol' | null>>({});

  // --- Substitutes DataTable source ---
  const subColumns: ColumnSpec<LineItemSubstituteItem>[] = [
    { key: 'priority',        header: '#',            type: 'numeric', sortable: false, align: 'right',
      cellText: (value) => String((value as number) + 1) },
    { key: 'part_mpn',        header: 'MPN',          type: 'text',    sortable: false },
    { key: 'manufacturer',    header: 'Manufacturer', type: 'text',    sortable: false },
    { key: 'description',     header: 'Description',  type: 'text',    sortable: false },
    { key: 'lifecycle_status',header: 'Status',       type: 'text',    sortable: false },
    { key: 'quantity_on_hand',header: 'Qty',          type: 'numeric', sortable: false, align: 'right' },
    { key: '_actions',        header: '',             type: 'text',    sortable: false, exportOnly: false },
  ];

  const subSource = new LocalDataSource<LineItemSubstituteItem>(subColumns, []);
  subSource.exportEnabled = false;

  $effect(() => {
    if (substitutesQuery.data) {
      subSource.setData(substitutesQuery.data.items);
    }
  });

  // --- Category pre-select ---
  $effect(() => {
    const cat = primaryPartQuery.data?.category_id;
    if (cat != null) {
      untrack(() => partsTableRef?.setCategoryFilter(cat));
    }
  });

  // --- Reset chip state on close ---
  $effect(() => {
    if (!open) chipModes = {};
  });

  function invalidate() {
    queryClient.invalidateQueries({ queryKey: ['revision', revisionId, 'item', lineItem.id, 'substitutes'] });
    queryClient.invalidateQueries({ queryKey: ['revision', teamId, projectId, revisionId] });
  }

  async function handleAddPart(part: PartSummary) {
    const items = substitutesQuery.data?.items ?? [];
    if (part.id === lineItem.resolved_part_id) {
      pushToast('warning', 'Cannot add the primary part as a substitute.');
      return;
    }
    if (items.some(s => s.part_id === part.id)) {
      pushToast('info', 'Part is already a substitute for this line item.');
      return;
    }
    try {
      await addSubstitute(teamId, projectId, revisionId, lineItem.id, part.id);
      invalidate();
      pushToast('success', `Added ${part.mpn ?? part.id} as substitute.`);
    } catch (err: any) {
      pushToast('error', 'Failed to add substitute.', err?.message);
    }
  }

  async function handleRemove(partId: string) {
    removing[partId] = true;
    const prev = subSource.rows.slice();
    subSource.setData(prev.filter(s => s.part_id !== partId));
    try {
      await removeSubstitute(teamId, projectId, revisionId, lineItem.id, partId);
      invalidate();
    } catch (err: any) {
      subSource.setData(prev);
      pushToast('error', 'Failed to remove substitute.', err?.message);
    } finally {
      removing[partId] = false;
    }
  }

  async function handleMove(partId: string, direction: 'up' | 'down') {
    const items = subSource.rows.slice();
    const idx = items.findIndex(s => s.part_id === partId);
    if (idx === -1) return;
    const swapWith = direction === 'up' ? idx - 1 : idx + 1;
    if (swapWith < 0 || swapWith >= items.length) return;
    const newItems = [...items];
    [newItems[idx], newItems[swapWith]] = [newItems[swapWith], newItems[idx]];
    reordering = true;
    subSource.setData(newItems);
    try {
      await reorderSubstitutes(teamId, projectId, revisionId, lineItem.id, newItems.map(s => s.part_id));
      invalidate();
    } catch (err: any) {
      subSource.setData(items);
      pushToast('error', 'Failed to reorder substitutes.', err?.message);
    } finally {
      reordering = false;
    }
  }

  async function handleImport() {
    if (!selectedImportRevId) return;
    importing = true;
    try {
      await importSubstitutes(teamId, projectId, revisionId, lineItem.id, selectedImportRevId);
      invalidate();
      pushToast('success', 'Substitutes imported.');
    } catch (err: any) {
      pushToast('error', 'Failed to import substitutes.', err?.message);
    } finally {
      importing = false;
    }
  }

  // --- Suggested filters ---
  type SuggestedFilter = {
    slug: string;
    displayName: string;
    unitSymbol: string | null;
    type: 'numeric' | 'enum';
    value: number | string;
  };

  const suggestedFilters = $derived.by((): SuggestedFilter[] => {
    const part = primaryPartQuery.data;
    if (!part) return [];
    const all = [
      ...(part.parameters?.user_specified ?? []),
      ...(part.parameters?.distributor_sourced ?? []),
    ];
    const seen = new Set<string>();
    const result: SuggestedFilter[] = [];
    for (const p of all) {
      if (!p.parameter_slug || p.parameter_id == null || seen.has(p.parameter_slug)) continue;
      seen.add(p.parameter_slug);
      if (p.numeric_value != null) {
        result.push({
          slug: p.parameter_slug,
          displayName: p.parameter_display_name ?? p.name,
          unitSymbol: p.parameter_unit_symbol ?? null,
          type: 'numeric',
          value: p.numeric_value,
        });
      } else if (p.value) {
        result.push({
          slug: p.parameter_slug,
          displayName: p.parameter_display_name ?? p.name,
          unitSymbol: null,
          type: 'enum',
          value: p.value,
        });
      }
    }
    return result;
  });

  function formatFilterValue(sf: SuggestedFilter): string {
    if (sf.type === 'numeric') {
      return sf.unitSymbol ? formatSI(sf.value as number, sf.unitSymbol) : String(sf.value);
    }
    return String(sf.value);
  }

  function encodeFilter(sf: SuggestedFilter, mode: 'eq' | 'gte' | 'lte' | 'tol'): string {
    const v = sf.value as number;
    if (mode === 'eq')  return `=${v}`;
    if (mode === 'gte') return `>=${v}`;
    if (mode === 'lte') return `<=${v}`;
    // tol: ±20%
    return `${v * 0.8}|${v * 1.2}`;
  }

  function toggleMode(sf: SuggestedFilter, mode: 'eq' | 'gte' | 'lte' | 'tol') {
    if (chipModes[sf.slug] === mode) {
      chipModes = { ...chipModes, [sf.slug]: null };
      partsTableRef?.setFilter(`param_${sf.slug}`, new Set());
    } else {
      chipModes = { ...chipModes, [sf.slug]: mode };
      partsTableRef?.setFilter(`param_${sf.slug}`, new Set([encodeFilter(sf, mode)]));
    }
  }

  function toggleEnum(sf: SuggestedFilter) {
    if (chipModes[sf.slug] === 'eq') {
      chipModes = { ...chipModes, [sf.slug]: null };
      partsTableRef?.setFilter(`param_${sf.slug}`, new Set());
    } else {
      chipModes = { ...chipModes, [sf.slug]: 'eq' };
      partsTableRef?.setFilter(`param_${sf.slug}`, new Set([String(sf.value)]));
    }
  }
</script>

{#snippet renderMpn(_value: unknown, row: LineItemSubstituteItem)}
  <PartRef partId={row.part_id} mpn={row.part_mpn} {teamId} fallback={row.description} />
{/snippet}

{#snippet renderLifecycle(value: unknown)}
  <LifecycleBadge status={value as string} />
{/snippet}

{#snippet renderQty(_value: unknown, row: LineItemSubstituteItem)}
  <span class="tabular-nums">{row.quantity_on_hand != null ? row.quantity_on_hand : '—'}</span>
{/snippet}

{#snippet renderActions(_value: unknown, row: LineItemSubstituteItem)}
  {@const items = subSource.rows}
  {@const idx = items.findIndex(s => s.part_id === row.part_id)}
  {@const isFirst = idx === 0}
  {@const isLast = idx === items.length - 1}
  <div class="flex items-center gap-1 justify-end">
    <Button variant="ghost" size="icon" class="h-7 w-7" disabled={isFirst || reordering}
      onclick={() => handleMove(row.part_id, 'up')} title="Move up">
      <ArrowUp class="h-3.5 w-3.5" />
    </Button>
    <Button variant="ghost" size="icon" class="h-7 w-7" disabled={isLast || reordering}
      onclick={() => handleMove(row.part_id, 'down')} title="Move down">
      <ArrowDown class="h-3.5 w-3.5" />
    </Button>
    <Button variant="ghost" size="icon" class="h-7 w-7 text-destructive hover:text-destructive"
      disabled={removing[row.part_id]}
      onclick={() => handleRemove(row.part_id)} title="Remove">
      <Trash2 class="h-3.5 w-3.5" />
    </Button>
  </div>
{/snippet}

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex w-[90vw] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
      style="height: min(85vh, 720px);"
    >
      <!-- Fixed header -->
      <div class="shrink-0 border-b border-border px-6 pb-4 pt-5">
        <div class="flex items-start justify-between gap-4">
          <div class="space-y-0.5">
            <Dialog.Title class="text-lg font-semibold">Manage Substitutes</Dialog.Title>
            <p class="text-sm text-muted-foreground">
              <span class="font-mono">{lineItem.reference_designators}</span>
              {#if lineItem.resolved_part_mpn}
                — primary: <span class="font-mono">{lineItem.resolved_part_mpn}</span>
              {/if}
            </p>
          </div>
          <Dialog.Close class="rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 mt-0.5">
            <X class="h-4 w-4" />
            <span class="sr-only">Close</span>
          </Dialog.Close>
        </div>
      </div>

      <!-- Tabs: list is fixed, content area scrolls -->
      <Tabs value="current" class="flex flex-col flex-1 min-h-0">
        <TabsList class="shrink-0 mx-6 mt-4 mb-0 self-start">
          <TabsTrigger value="current">
            Current
            {#if substitutesQuery.data && substitutesQuery.data.items.length > 0}
              <Badge variant="secondary" class="ml-1.5 text-xs">{substitutesQuery.data.items.length}</Badge>
            {/if}
          </TabsTrigger>
          <TabsTrigger value="import">Import from Previous</TabsTrigger>
          <TabsTrigger value="catalog">Add from Catalog</TabsTrigger>
        </TabsList>

        <!-- Tab 1: Current substitutes -->
        <TabsContent value="current" class="mt-0 flex-1 min-h-0 overflow-auto px-6 py-4">
          {#if substitutesQuery.isPending}
            <p class="text-sm text-muted-foreground">Loading…</p>
          {:else if substitutesQuery.isError}
            <p class="text-sm text-destructive">Failed to load substitutes.</p>
          {:else if subSource.rows.length === 0}
            <p class="text-sm text-muted-foreground py-6 text-center">No substitutes configured. Use the other tabs to add some.</p>
          {:else}
            <p class="text-xs text-muted-foreground mb-3">Substitutes are tried in order — the first with sufficient free stock is used.</p>
            <DataTable
              source={subSource}
              getRowKey={(r) => r.part_id}
              renderers={{
                part_mpn: renderMpn,
                lifecycle_status: renderLifecycle,
                quantity_on_hand: renderQty,
                _actions: renderActions,
              }}
              emptyMessage="No substitutes"
            />
          {/if}
        </TabsContent>

        <!-- Tab 2: Import from previous revision -->
        <TabsContent value="import" class="mt-0 flex-1 min-h-0 overflow-auto px-6 py-4">
          {#if previousRevisionsQuery.isPending}
            <p class="text-sm text-muted-foreground">Loading…</p>
          {:else if previousRevisionsQuery.isError}
            <p class="text-sm text-destructive">Failed to load previous revisions.</p>
          {:else if previousRevisionsQuery.data.items.length === 0}
            <p class="text-sm text-muted-foreground py-6 text-center">
              No previous revisions with substitutes found for the same BOM line item (matched by resolved part).
            </p>
          {:else}
            <p class="text-sm text-muted-foreground mb-4">
              Select a previous revision to copy its substitutes for this line item.
            </p>
            <div class="space-y-2 mb-5">
              {#each previousRevisionsQuery.data.items as rev (rev.id)}
                <label class="flex items-center gap-3 p-3 rounded-md border border-border cursor-pointer hover:bg-accent/50 transition-colors has-[:checked]:border-primary has-[:checked]:bg-primary/5">
                  <input
                    type="radio"
                    name="import-revision"
                    value={rev.id}
                    bind:group={selectedImportRevId}
                    class="accent-primary"
                  />
                  <div>
                    <span class="text-sm font-medium">{rev.label}</span>
                    <span class="text-xs text-muted-foreground ml-2">{new Date(rev.imported_at).toLocaleDateString()}</span>
                  </div>
                </label>
              {/each}
            </div>
            <Button
              size="sm"
              disabled={!selectedImportRevId || importing}
              onclick={handleImport}
            >
              <Download class="h-4 w-4 mr-1.5" />
              {importing ? 'Importing…' : 'Import Substitutes'}
            </Button>
          {/if}
        </TabsContent>

        <!-- Tab 3: Add from catalog -->
        <TabsContent value="catalog" class="mt-0 flex-1 min-h-0 overflow-auto px-6 pt-3 pb-6">
          {#if suggestedFilters.length > 0}
            <div class="flex flex-wrap items-center gap-1.5 mb-3">
              <span class="text-xs text-muted-foreground shrink-0">Match primary part:</span>
              {#each suggestedFilters as sf (sf.slug)}
                {@const activeMode = chipModes[sf.slug] ?? null}
                {@const isActive = activeMode != null}
                {#if sf.type === 'numeric'}
                  <div class={cn(
                    'group inline-flex items-center gap-1 rounded-full border text-xs px-2 py-0.5 transition-colors cursor-default',
                    isActive ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground',
                  )}>
                    <span>{sf.displayName}: {formatFilterValue(sf)}</span>
                    <span class="hidden group-hover:inline-flex items-center gap-0.5 ml-1">
                      {#each (['eq', 'gte', 'lte', 'tol'] as const) as mode}
                        <button
                          class={cn(
                            'rounded px-1 text-[10px] leading-none border transition-colors',
                            activeMode === mode
                              ? 'border-primary bg-primary text-primary-foreground'
                              : 'border-border hover:border-foreground/40',
                          )}
                          onclick={() => toggleMode(sf, mode)}
                          title={{ eq: 'Exact', gte: 'At least', lte: 'At most', tol: '±20%' }[mode]}
                        >{{ eq: '=', gte: '≥', lte: '≤', tol: '~' }[mode]}</button>
                      {/each}
                    </span>
                  </div>
                {:else}
                  <button
                    class={cn(
                      'inline-flex items-center rounded-full border text-xs px-2 py-0.5 transition-colors',
                      isActive ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:border-foreground/40',
                    )}
                    onclick={() => toggleEnum(sf)}
                  >{sf.displayName}: {sf.value}</button>
                {/if}
              {/each}
            </div>
          {/if}
          <p class="text-xs text-muted-foreground mb-3">Click a part to add it as a substitute.</p>
          <PartsTable
            bind:this={partsTableRef}
            {teamId}
            onRowClick={handleAddPart}
            syncUrl={false}
          />
        </TabsContent>
      </Tabs>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

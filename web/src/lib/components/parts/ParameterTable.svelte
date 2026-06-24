<script lang="ts">
  import type { Parameter, Listing } from '$lib/types/parts.js';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { createParameter, deleteParameter } from '$lib/api/parts.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus, Trash2, Pencil, Check, X } from 'lucide-svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';

  let {
    partId,
    parameters,
    preferredSourceId,
    listings,
    onchange,
  }: {
    partId: string;
    parameters: { user_specified: Parameter[]; distributor_sourced: Parameter[] };
    preferredSourceId?: string | null;
    listings?: Listing[];
    onchange?: () => void;
  } = $props();

  type Row = Parameter & { displaySource?: string };

  // Inline edit state — tracks which param row is being edited
  let editingParamId = $state<string | null>(null);
  let editName = $state('');
  let editValue = $state('');
  let editUnit = $state('');
  let editNameReadonly = $state(false); // true when overriding a distributor param

  // Add-new state (compact inline form below the active tab)
  let addingParam = $state(false);
  let addingFrom = $state<'user' | 'combined'>('user');
  let newName = $state('');
  let newValue = $state('');
  let newUnit = $state('');

  function startInlineEdit(param: Parameter, nameReadonly: boolean) {
    addingParam = false;
    editingParamId = param.id;
    editName = param.name;
    editValue = param.value;
    editUnit = param.unit ?? '';
    editNameReadonly = nameReadonly;
  }

  function cancelInlineEdit() {
    editingParamId = null;
  }

  async function saveInlineEdit() {
    if (!editName.trim() || !editValue.trim()) return;
    try {
      await createParameter(partId, { name: editName.trim(), value: editValue.trim(), unit: editUnit.trim() || undefined });
      editingParamId = null;
      onchange?.();
    } catch (e: any) {
      pushToast('error', 'Failed to save parameter', e.message);
    }
  }

  function startAdd(from: 'user' | 'combined') {
    editingParamId = null;
    newName = '';
    newValue = '';
    newUnit = '';
    addingFrom = from;
    addingParam = true;
  }

  function cancelAdd() {
    addingParam = false;
    newName = '';
    newValue = '';
    newUnit = '';
  }

  async function addParam() {
    if (!newName.trim() || !newValue.trim()) return;
    try {
      await createParameter(partId, { name: newName.trim(), value: newValue.trim(), unit: newUnit.trim() || undefined });
      cancelAdd();
      onchange?.();
    } catch (e: any) {
      pushToast('error', 'Failed to add parameter', e.message);
    }
  }

  function handleEditKey(e: KeyboardEvent) {
    if (e.key === 'Enter') saveInlineEdit();
    else if (e.key === 'Escape') cancelInlineEdit();
  }

  function handleAddKey(e: KeyboardEvent) {
    if (e.key === 'Enter') addParam();
    else if (e.key === 'Escape') cancelAdd();
  }

  async function removeParam(paramId: string) {
    try {
      await deleteParameter(partId, paramId);
      onchange?.();
    } catch (e: any) {
      pushToast('error', 'Failed to delete parameter', e.message);
    }
  }

  // Combined parameter list with priority: user > preferred distributor > all
  // distributors, sorted by display label for a scannable datasheet-style read.
  // A user row supersedes any distributor row sharing its canonical parameter_id
  // (when mapped) or, as a fallback for unmapped/free-text params, its raw name.
  let combined = $derived.by<Row[]>(() => {
    const userRows: Row[] = (parameters.user_specified ?? []).map((p) => ({ ...p, displaySource: 'User' }));
    const userIds = new Set(
      userRows.map((p) => p.parameter_id).filter((id): id is number => id != null),
    );
    const userNames = new Set(userRows.map((p) => p.name));
    const superseded = (p: Parameter) =>
      (p.parameter_id != null && userIds.has(p.parameter_id)) || userNames.has(p.name);

    const distRows: Row[] = (parameters.distributor_sourced ?? [])
      .filter((p) => !superseded(p))
      .filter((p) => !preferredSourceId || p.source_distributor_id === preferredSourceId)
      .map((p) => ({ ...p, displaySource: p.source_distributor_name ?? '' }));

    return [...userRows, ...distRows].sort((a, b) =>
      paramLabel(a).toLowerCase().localeCompare(paramLabel(b).toLowerCase()),
    );
  });

  function listingTabLabel(listing: Listing): string {
    const siblings = (listings ?? []).filter((l) => l.distributor_id === listing.distributor_id);
    if (siblings.length <= 1) return listing.distributor_name;
    return `${listing.distributor_name} (${listing.packaging_type ?? listing.sku})`;
  }

  /** Display label for a parameter name: use canonical name when available. */
  function paramLabel(param: Parameter): string {
    return param.parameter_display_name ?? param.name;
  }

  /**
   * Display value: the original, unaltered parameter text — with the unit appended
   * when it is stored separately (as for user-specified params; distributor values
   * already embed their unit). Normalization is deliberately not applied here: the
   * filters and sorting use the normalized numeric values, but the table shows
   * exactly what the source reported (e.g. "100 Ohm @ 1 MHz", not "100 Ω").
   */
  function paramValue(param: Parameter): string {
    return param.unit ? `${param.value} ${param.unit}` : param.value;
  }

  /** Single-letter source marker (U for user, first letter of the distributor). */
  function sourceMark(displaySource: string): string {
    return displaySource === 'User' ? 'U' : (displaySource[0] ?? '?').toUpperCase();
  }
</script>

<!--
  Parameters flow into a width-driven number of columns (CSS multi-column):
  one column per ~800px of available width, balanced column-major so the
  alphabetically-sorted list still reads top-to-bottom within each column.
-->
{#snippet editRow()}
  <div class="flex items-center gap-1 break-inside-avoid border-b border-border/40 py-1">
    <input
      bind:value={editName}
      placeholder="name"
      readonly={editNameReadonly}
      onkeydown={handleEditKey}
      class="w-2/5 rounded border border-input bg-background px-2 py-0.5 text-xs font-mono {editNameReadonly ? 'opacity-60' : ''}"
    />
    <input bind:value={editValue} placeholder="value" onkeydown={handleEditKey} class="flex-1 rounded border border-input bg-background px-2 py-0.5 text-xs" />
    <input bind:value={editUnit} placeholder="unit" onkeydown={handleEditKey} class="w-16 rounded border border-input bg-background px-2 py-0.5 text-xs" />
    <button type="button" title="Save" onclick={saveInlineEdit} class="rounded p-1 hover:bg-muted"><Check class="h-3.5 w-3.5 text-success" /></button>
    <button type="button" title="Cancel" onclick={cancelInlineEdit} class="rounded p-1 hover:bg-muted"><X class="h-3.5 w-3.5" /></button>
  </div>
{/snippet}

{#snippet specList(rows: Row[], editable: boolean, showSource: boolean)}
  <div class="columns-[800px] gap-x-10 text-sm">
    {#each rows as param (param.id)}
      {#if editable && editingParamId === param.id}
        {@render editRow()}
      {:else}
        <div class="group flex items-center gap-2 break-inside-avoid border-b border-border/40 py-1">
          <span class="w-2/5 shrink-0 truncate text-muted-foreground" title={paramLabel(param)}>{paramLabel(param)}</span>
          <span class="min-w-0 flex-1 font-medium tabular-nums">{paramValue(param)}</span>
          {#if showSource}
            <span
              class="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded text-[10px] font-medium {param.displaySource === 'User' ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}"
              title={param.displaySource}
            >{sourceMark(param.displaySource ?? '')}</span>
          {/if}
          {#if editable}
            <button
              type="button"
              title={param.displaySource === 'User' ? 'Edit' : 'Override with user value'}
              onclick={() => startInlineEdit(param, param.displaySource !== 'User')}
              class="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition hover:bg-muted hover:text-foreground group-hover:opacity-100 focus:opacity-100"
            >
              <Pencil class="h-3.5 w-3.5" />
            </button>
          {/if}
        </div>
      {/if}
    {/each}
  </div>
{/snippet}

{#snippet addForm()}
  <div class="mt-2 flex items-center gap-1">
    <input bind:value={newName} placeholder="name" onkeydown={handleAddKey} class="w-2/5 rounded border border-input bg-background px-2 py-0.5 text-xs font-mono" />
    <input bind:value={newValue} placeholder="value" onkeydown={handleAddKey} class="flex-1 rounded border border-input bg-background px-2 py-0.5 text-xs" />
    <input bind:value={newUnit} placeholder="unit" onkeydown={handleAddKey} class="w-16 rounded border border-input bg-background px-2 py-0.5 text-xs" />
    <Button size="sm" onclick={addParam}>Save</Button>
    <Button variant="ghost" size="sm" onclick={cancelAdd}>Cancel</Button>
  </div>
{/snippet}

<Tabs value="combined">
  <TabsList>
    <TabsTrigger value="combined">Unified</TabsTrigger>
    <TabsTrigger value="user">User ({parameters.user_specified?.length ?? 0})</TabsTrigger>
    {#each (listings ?? []) as listing (listing.id)}
      <TabsTrigger value="dist-{listing.id}">{listingTabLabel(listing)}</TabsTrigger>
    {/each}
  </TabsList>

  <!-- Combined parameters tab -->
  <TabsContent value="combined">
    {#if combined.length === 0}
      <p class="py-6 text-center text-sm text-muted-foreground">No parameters</p>
    {:else}
      {@render specList(combined, true, true)}
    {/if}
    {#if addingParam && addingFrom === 'combined'}
      {@render addForm()}
    {:else}
      <div class="mt-2">
        <Button variant="outline" size="sm" onclick={() => startAdd('combined')}>
          <Plus class="mr-1 h-3.5 w-3.5" /> Add parameter
        </Button>
      </div>
    {/if}
  </TabsContent>

  <!-- User-specified parameters tab -->
  <TabsContent value="user">
    {#if (parameters.user_specified ?? []).length === 0}
      <p class="py-6 text-center text-sm text-muted-foreground">No user parameters</p>
    {:else}
      <div class="columns-[800px] gap-x-10 text-sm">
        {#each (parameters.user_specified ?? []) as param (param.id)}
          {#if editingParamId === param.id}
            {@render editRow()}
          {:else}
            <div class="group flex items-center gap-2 break-inside-avoid border-b border-border/40 py-1">
              <span class="w-2/5 shrink-0 truncate font-mono text-xs text-muted-foreground" title={param.name}>{param.name}</span>
              <span class="min-w-0 flex-1 font-medium">{param.unit ? `${param.value} ${param.unit}` : param.value}</span>
              <div class="flex shrink-0 items-center gap-0.5 opacity-0 transition group-hover:opacity-100 focus-within:opacity-100">
                <button type="button" title="Edit" onclick={() => startInlineEdit(param, false)} class="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"><Pencil class="h-3.5 w-3.5" /></button>
                <button type="button" title="Delete" onclick={() => removeParam(param.id)} class="rounded p-1 text-muted-foreground hover:bg-muted hover:text-destructive"><Trash2 class="h-3.5 w-3.5" /></button>
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}
    {#if addingParam && addingFrom === 'user'}
      {@render addForm()}
    {:else}
      <div class="mt-2">
        <Button variant="outline" size="sm" onclick={() => startAdd('user')}>
          <Plus class="mr-1 h-3.5 w-3.5" /> Add parameter
        </Button>
      </div>
    {/if}
  </TabsContent>

  <!-- Per-listing distributor parameter tabs -->
  {#each (listings ?? []) as listing (listing.id)}
    {@const distParams = (parameters.distributor_sourced ?? [])
      .filter((p) => p.source_distributor_id === listing.distributor_id)
      .sort((a, b) => paramLabel(a).toLowerCase().localeCompare(paramLabel(b).toLowerCase()))}
    <TabsContent value="dist-{listing.id}">
      <div class="mb-3 flex gap-6 px-1 text-sm text-muted-foreground">
        <span>SKU: <span class="font-mono text-foreground">{listing.sku}</span></span>
        {#if listing.packaging_type}<span>Packaging: {listing.packaging_type}</span>{/if}
        {#if listing.unit_price != null}<span>Price: <PriceDisplay value={listing.unit_price} decimals={4} /></span>{/if}
      </div>
      {#if distParams.length === 0}
        <p class="py-6 text-center text-sm text-muted-foreground">No parameters from this distributor</p>
      {:else}
        {@render specList(distParams, false, false)}
      {/if}
    </TabsContent>
  {/each}
</Tabs>

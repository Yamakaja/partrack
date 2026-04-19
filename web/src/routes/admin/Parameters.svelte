<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    listParameterDefinitions,
    createParameterDefinition,
    updateParameterDefinition,
    deleteParameterDefinition,
    listDistributorParamMappings,
    updateDistributorParamMapping,
  } from '$lib/api/admin.js';
  import type { ParameterDefinition, DistributorParamMapping } from '$lib/types/parts.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog, Popover } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Plus, Link, EyeOff, Wand2, Trash2 } from 'lucide-svelte';

  const qc = useQueryClient();

  const defsQuery = createQuery(() => ({
    queryKey: ['admin', 'parameter-definitions'],
    queryFn: () => listParameterDefinitions(),
  }));

  const unmappedQuery = createQuery(() => ({
    queryKey: ['admin', 'distributor-param-mappings', 'unmapped'],
    queryFn: () => listDistributorParamMappings({ unmapped: true }),
  }));

  const allMappingsQuery = createQuery(() => ({
    queryKey: ['admin', 'distributor-param-mappings', 'all'],
    queryFn: () => listDistributorParamMappings(),
  }));

  // ── Add parameter definition dialog ─────────────────────────────────────────
  let addOpen = $state(false);
  let addSlug = $state('');
  let addDisplayName = $state('');
  let addUnit = $state('');
  let addUnitSymbol = $state('');
  let addValueType = $state<ParameterDefinition['value_type']>('numeric');
  let adding = $state(false);
  let pendingLinkMappingId = $state<string | null>(null);

  function openAdd() {
    addSlug = '';
    addDisplayName = '';
    addUnit = '';
    addUnitSymbol = '';
    addValueType = 'numeric';
    pendingLinkMappingId = null;
    addOpen = true;
  }

  function openMakeNew(m: DistributorParamMapping) {
    addDisplayName = m.distributor_param_name;
    addSlug = m.distributor_param_name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '_')
      .replace(/^_+|_+$/g, '');
    addUnit = '';
    addUnitSymbol = '';
    addValueType = 'numeric';
    pendingLinkMappingId = m.id;
    addOpen = true;
  }

  async function handleAdd(e: Event) {
    e.preventDefault();
    adding = true;
    try {
      const newDef = await createParameterDefinition({
        slug: addSlug,
        display_name: addDisplayName,
        unit: addUnit || null,
        unit_symbol: addUnitSymbol || null,
        value_type: addValueType,
      });
      if (pendingLinkMappingId) {
        await updateDistributorParamMapping(pendingLinkMappingId, { parameter_id: newDef.id });
        qc.invalidateQueries({ queryKey: ['admin', 'distributor-param-mappings'] });
        pushToast('success', `Created and linked "${newDef.display_name}"`);
      } else {
        pushToast('success', 'Parameter definition created');
      }
      addOpen = false;
      pendingLinkMappingId = null;
      qc.invalidateQueries({ queryKey: ['admin', 'parameter-definitions'] });
    } catch (err: any) {
      pushToast('error', 'Failed to create parameter', err.message);
    } finally {
      adding = false;
    }
  }

  // ── Edit parameter definition dialog ─────────────────────────────────────────
  let editOpen = $state(false);
  let editDef = $state<ParameterDefinition | null>(null);
  let editSlug = $state('');
  let editDisplayName = $state('');
  let editUnit = $state('');
  let editUnitSymbol = $state('');
  let editValueType = $state<ParameterDefinition['value_type']>('numeric');
  let editing = $state(false);

  function openEdit(d: ParameterDefinition) {
    editDef = d;
    editSlug = d.slug;
    editDisplayName = d.display_name;
    editUnit = d.unit ?? '';
    editUnitSymbol = d.unit_symbol ?? '';
    editValueType = d.value_type;
    editOpen = true;
  }

  async function handleEdit(e: Event) {
    e.preventDefault();
    if (!editDef) return;
    editing = true;
    try {
      await updateParameterDefinition(editDef.id, {
        slug: editSlug,
        display_name: editDisplayName,
        unit: editUnit || null,
        unit_symbol: editUnitSymbol || null,
        value_type: editValueType,
      });
      editOpen = false;
      qc.invalidateQueries({ queryKey: ['admin', 'parameter-definitions'] });
      pushToast('success', 'Parameter definition updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update parameter', err.message);
    } finally {
      editing = false;
    }
  }

  // ── Delete parameter definition ──────────────────────────────────────────────
  let deleteConfirmOpen = $state(false);
  let deletingDef = $state<ParameterDefinition | null>(null);
  let deleting = $state(false);

  function promptDeleteDef(d: ParameterDefinition) {
    deletingDef = d;
    deleteConfirmOpen = true;
  }

  async function handleDeleteDef() {
    if (!deletingDef) return;
    deleting = true;
    try {
      await deleteParameterDefinition(deletingDef.id);
      qc.invalidateQueries({ queryKey: ['admin', 'parameter-definitions'] });
      pushToast('success', `Deleted "${deletingDef.display_name}"`);
    } catch (err: any) {
      pushToast('error', 'Failed to delete parameter definition', err.message);
    } finally {
      deleting = false;
      deletingDef = null;
    }
  }

  // ── Link parameter ────────────────────────────────────────────────────────────
  let linkOpen = $state<Record<string, boolean>>({});
  let linkSearch = $state<Record<string, string>>({});
  let linking = $state(false);

  async function handleLink(mapping: DistributorParamMapping, paramId: number) {
    linking = true;
    try {
      await updateDistributorParamMapping(mapping.id, { parameter_id: paramId });
      linkOpen[mapping.id] = false;
      qc.invalidateQueries({ queryKey: ['admin', 'distributor-param-mappings'] });
      pushToast('success', `Linked "${mapping.distributor_param_name}"`);
    } catch (err: any) {
      pushToast('error', 'Failed to link parameter', err.message);
    } finally {
      linking = false;
    }
  }

  async function handleIgnore(mapping: DistributorParamMapping) {
    try {
      await updateDistributorParamMapping(mapping.id, { ignored: true });
      qc.invalidateQueries({ queryKey: ['admin', 'distributor-param-mappings'] });
      pushToast('success', `Ignored "${mapping.distributor_param_name}"`);
    } catch (err: any) {
      pushToast('error', 'Failed to ignore parameter', err.message);
    }
  }

  async function handleUnignore(mapping: DistributorParamMapping) {
    try {
      await updateDistributorParamMapping(mapping.id, { ignored: false });
      qc.invalidateQueries({ queryKey: ['admin', 'distributor-param-mappings'] });
      pushToast('success', `Unignored "${mapping.distributor_param_name}"`);
    } catch (err: any) {
      pushToast('error', 'Failed to unignore parameter', err.message);
    }
  }

  function focusAndSelect(node: HTMLInputElement) {
    node.focus();
    node.select();
  }
</script>

{#snippet linkPopoverBtn(m: DistributorParamMapping, label: string)}
  {@const search = linkSearch[m.id] ?? ''}
  {@const filteredDefs = (defsQuery.data ?? []).filter((d) =>
    `${d.display_name} ${d.slug}`.toLowerCase().includes(search.trim().toLowerCase()),
  )}
  <Popover.Root
    open={linkOpen[m.id] ?? false}
    onOpenChange={(v) => {
      if (v) linkSearch[m.id] = m.distributor_param_name;
      else linkSearch[m.id] = '';
      linkOpen[m.id] = v;
    }}
  >
    <Popover.Trigger>
      <Button variant="outline" size="sm">
        <Link class="h-3 w-3 mr-1" />
        {label}
      </Button>
    </Popover.Trigger>
    <Popover.Content
      align="end"
      class="z-50 w-64 rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    >
      <div class="border-b border-border px-2 py-1.5">
        <input
          type="search"
          value={linkSearch[m.id] ?? ''}
          oninput={(e) => { linkSearch[m.id] = (e.target as HTMLInputElement).value; }}
          placeholder="Search parameters…"
          class="w-full bg-transparent text-xs placeholder:text-muted-foreground focus:outline-none"
          use:focusAndSelect
        />
      </div>
      <div class="max-h-48 overflow-y-auto py-1">
        {#if filteredDefs.length === 0}
          <p class="px-2 py-1.5 text-xs text-muted-foreground">No results</p>
        {:else}
          {#each filteredDefs as d (d.id)}
            <button
              class="flex w-full items-center gap-1.5 rounded-sm px-2 py-1.5 text-xs hover:bg-accent hover:text-accent-foreground text-left disabled:opacity-50"
              disabled={linking}
              onmousedown={(e) => { e.preventDefault(); handleLink(m, d.id); }}
            >
              <span class="font-medium">{d.display_name}</span>
              <span class="text-muted-foreground font-mono">({d.slug}) {#if d.unit_symbol}
                {d.unit_symbol}
              {/if}</span>
            </button>
          {/each}
        {/if}
      </div>
      <div class="border-t border-border px-2 py-1.5">
        <button
          class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
          onmousedown={(e) => { e.preventDefault(); linkOpen[m.id] = false; linkSearch[m.id] = ''; }}
        >
          Cancel
        </button>
      </div>
    </Popover.Content>
  </Popover.Root>
{/snippet}

<div class="flex flex-col h-full">
  <Topbar title="Parameter Registry">
    {#snippet actions()}
      <Button size="sm" onclick={openAdd}>
        <Plus class="h-4 w-4 mr-1" /> Add Parameter
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    <Tabs value="definitions">
      <TabsList class="mb-4">
        <TabsTrigger value="definitions">Parameter Definitions</TabsTrigger>
        <TabsTrigger value="unmapped">
          Unmapped
          {#if (unmappedQuery.data?.length ?? 0) > 0}
            <span class="ml-1.5 inline-flex items-center rounded-full bg-destructive/15 px-1.5 py-0.5 text-xs font-medium text-destructive">
              {unmappedQuery.data?.length}
            </span>
          {/if}
        </TabsTrigger>
        <TabsTrigger value="mappings">Mappings</TabsTrigger>
      </TabsList>

      <!-- Tab 1: Parameter Definitions -->
      <TabsContent value="definitions">
        {#if defsQuery.isPending}
          <div class="text-sm text-muted-foreground">Loading…</div>
        {:else if defsQuery.isError}
          <div class="text-sm text-destructive">Error: {defsQuery.error.message}</div>
        {:else if defsQuery.data}
          {#if defsQuery.data.length === 0}
            <div class="text-sm text-muted-foreground py-8 text-center">No parameter definitions yet.</div>
          {:else}
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Slug</TableHead>
                  <TableHead>Display Name</TableHead>
                  <TableHead>Unit</TableHead>
                  <TableHead>Symbol</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each defsQuery.data as d (d.id)}
                  <TableRow>
                    <TableCell class="text-muted-foreground text-xs">{d.id}</TableCell>
                    <TableCell class="font-mono text-sm">{d.slug}</TableCell>
                    <TableCell class="font-medium">{d.display_name}</TableCell>
                    <TableCell class="text-muted-foreground text-sm">{d.unit ?? '—'}</TableCell>
                    <TableCell class="text-sm">{d.unit_symbol ?? '—'}</TableCell>
                    <TableCell>
                      <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-muted text-muted-foreground">
                        {d.value_type}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div class="flex items-center gap-1 justify-end">
                        <Button variant="ghost" size="sm" onclick={() => openEdit(d)}>Edit</Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          class="text-destructive hover:text-destructive"
                          onclick={() => promptDeleteDef(d)}
                        >
                          <Trash2 class="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          {/if}
        {/if}
      </TabsContent>

      <!-- Tab 2: Unmapped Parameters -->
      <TabsContent value="unmapped">
        {#if unmappedQuery.isPending}
          <div class="text-sm text-muted-foreground">Loading…</div>
        {:else if unmappedQuery.isError}
          <div class="text-sm text-destructive">Error: {unmappedQuery.error.message}</div>
        {:else if unmappedQuery.data}
          {#if unmappedQuery.data.length === 0}
            <div class="text-sm text-muted-foreground py-8 text-center">
              All distributor parameters are mapped. Run a part enrichment to discover new ones.
            </div>
          {:else}
            <p class="text-sm text-muted-foreground mb-4">
              {unmappedQuery.data.length} unmapped distributor parameter{unmappedQuery.data.length === 1 ? '' : 's'}, sorted by affected part count.
              Link each to a partrack parameter definition to enable numeric search and display.
            </p>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Distributor</TableHead>
                  <TableHead>Parameter Name</TableHead>
                  <TableHead>Distributor ID</TableHead>
                  <TableHead class="text-right">Parts</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each unmappedQuery.data as m (m.id)}
                  <TableRow>
                    <TableCell class="text-sm font-medium">{m.distributor_name}</TableCell>
                    <TableCell>
                      <div class="font-mono text-sm">{m.distributor_param_name}</div>
                      {#if m.sample_values.length > 0}
                        <div class="flex flex-wrap gap-1 mt-1">
                          {#each m.sample_values as v}
                            <span class="inline-block max-w-[12rem] truncate rounded px-1.5 py-0.5 text-xs font-mono bg-muted text-muted-foreground" title={v}>{v}</span>
                          {/each}
                        </div>
                      {/if}
                    </TableCell>
                    <TableCell class="text-muted-foreground text-xs">{m.distributor_param_id}</TableCell>
                    <TableCell class="text-right text-sm">{m.part_count}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-1 justify-end">
                        {@render linkPopoverBtn(m, 'Link')}
                        <Button
                          variant="outline"
                          size="sm"
                          onclick={() => openMakeNew(m)}
                        >
                          <Wand2 class="h-3 w-3 mr-1" /> Make New
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          class="text-muted-foreground"
                          onclick={() => handleIgnore(m)}
                        >
                          <EyeOff class="h-3 w-3 mr-1" /> Ignore
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          {/if}
        {/if}
      </TabsContent>

      <!-- Tab 3: All Mappings -->
      <TabsContent value="mappings">
        {#if allMappingsQuery.isPending}
          <div class="text-sm text-muted-foreground">Loading…</div>
        {:else if allMappingsQuery.isError}
          <div class="text-sm text-destructive">Error: {allMappingsQuery.error.message}</div>
        {:else if allMappingsQuery.data}
          {#if allMappingsQuery.data.length === 0}
            <div class="text-sm text-muted-foreground py-8 text-center">
              No distributor parameter mappings yet. Run a part enrichment to discover parameters.
            </div>
          {:else}
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Distributor</TableHead>
                  <TableHead>Parameter Name</TableHead>
                  <TableHead>Distributor ID</TableHead>
                  <TableHead>Linked To</TableHead>
                  <TableHead class="text-right">Parts</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each allMappingsQuery.data as m (m.id)}
                  {@const linkedDef = m.parameter_id != null
                    ? (defsQuery.data ?? []).find((d) => d.id === m.parameter_id)
                    : undefined}
                  <TableRow>
                    <TableCell class="text-sm font-medium">{m.distributor_name}</TableCell>
                    <TableCell>
                      <div class="font-mono text-sm">{m.distributor_param_name}</div>
                      {#if m.sample_values.length > 0}
                        <div class="flex flex-wrap gap-1 mt-1">
                          {#each m.sample_values as v}
                            <span class="inline-block max-w-[12rem] truncate rounded px-1.5 py-0.5 text-xs font-mono bg-muted text-muted-foreground" title={v}>{v}</span>
                          {/each}
                        </div>
                      {/if}
                    </TableCell>
                    <TableCell class="text-muted-foreground text-xs">{m.distributor_param_id}</TableCell>
                    <TableCell>
                      {#if linkedDef}
                        <span class="text-sm">{linkedDef.display_name}</span>
                        <span class="text-muted-foreground text-xs font-mono ml-1">({linkedDef.slug})</span>
                      {:else if m.ignored}
                        <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-muted text-muted-foreground">
                          Ignored
                        </span>
                      {:else}
                        <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-destructive/15 text-destructive">
                          Unmapped
                        </span>
                      {/if}
                    </TableCell>
                    <TableCell class="text-right text-sm">{m.part_count}</TableCell>
                    <TableCell>
                      <div class="flex items-center gap-1 justify-end">
                        {#if m.ignored}
                          <Button
                            variant="ghost"
                            size="sm"
                            class="text-muted-foreground"
                            onclick={() => handleUnignore(m)}
                          >
                            Unignore
                          </Button>
                        {:else if linkedDef}
                          {@render linkPopoverBtn(m, 'Relink')}
                          <Button
                            variant="ghost"
                            size="sm"
                            class="text-muted-foreground"
                            onclick={() => handleIgnore(m)}
                          >
                            <EyeOff class="h-3 w-3 mr-1" /> Ignore
                          </Button>
                        {:else}
                          {@render linkPopoverBtn(m, 'Link')}
                          <Button
                            variant="outline"
                            size="sm"
                            onclick={() => openMakeNew(m)}
                          >
                            <Wand2 class="h-3 w-3 mr-1" /> Make New
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            class="text-muted-foreground"
                            onclick={() => handleIgnore(m)}
                          >
                            <EyeOff class="h-3 w-3 mr-1" /> Ignore
                          </Button>
                        {/if}
                      </div>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          {/if}
        {/if}
      </TabsContent>
    </Tabs>
  </div>
</div>

<!-- Add Parameter Dialog -->
<Dialog.Root bind:open={addOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Add Parameter Definition</Dialog.Title>
      {#if pendingLinkMappingId}
        <p class="text-xs text-muted-foreground mb-4">Will be linked to the mapping after creation.</p>
      {:else}
        <div class="mb-4"></div>
      {/if}
      <form onsubmit={handleAdd} class="space-y-3">
        <div class="space-y-1">
          <Label for="add-slug">Slug <span class="text-destructive">*</span></Label>
          <Input id="add-slug" bind:value={addSlug} required placeholder="resistance" class="font-mono" />
          <p class="text-xs text-muted-foreground">Lowercase, underscores only (e.g. voltage_rating)</p>
        </div>
        <div class="space-y-1">
          <Label for="add-display">Display Name <span class="text-destructive">*</span></Label>
          <Input id="add-display" bind:value={addDisplayName} required placeholder="Resistance" />
        </div>
        <div class="grid grid-cols-2 gap-2">
          <div class="space-y-1">
            <Label for="add-unit">Unit identifier</Label>
            <Input id="add-unit" bind:value={addUnit} placeholder="ohm" />
          </div>
          <div class="space-y-1">
            <Label for="add-symbol">Display symbol</Label>
            <Input id="add-symbol" bind:value={addUnitSymbol} placeholder="Ω" />
          </div>
        </div>
        <div class="space-y-1">
          <Label for="add-type">Value type</Label>
          <select
            id="add-type"
            bind:value={addValueType}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="numeric">numeric</option>
            <option value="range">range</option>
            <option value="text">text</option>
            <option value="enum">enum</option>
          </select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { addOpen = false; pendingLinkMappingId = null; }}>Cancel</Button>
          <Button type="submit" disabled={adding || !addSlug || !addDisplayName}>
            {#if adding}
              {pendingLinkMappingId ? 'Creating & linking…' : 'Creating…'}
            {:else}
              {pendingLinkMappingId ? 'Create & Link' : 'Create'}
            {/if}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Delete Parameter Confirmation -->
<ConfirmDialog
  bind:open={deleteConfirmOpen}
  title="Delete Parameter Definition"
  description={deletingDef
    ? `Delete "${deletingDef.display_name}"? Distributor mappings and part parameters referencing it will lose their structured link.`
    : ''}
  confirmLabel={deleting ? 'Deleting…' : 'Delete'}
  destructive
  onconfirm={handleDeleteDef}
/>

<!-- Edit Parameter Dialog -->
<Dialog.Root bind:open={editOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Edit Parameter Definition</Dialog.Title>
      <form onsubmit={handleEdit} class="space-y-3">
        <div class="space-y-1">
          <Label for="edit-slug">Slug <span class="text-destructive">*</span></Label>
          <Input id="edit-slug" bind:value={editSlug} required class="font-mono" />
        </div>
        <div class="space-y-1">
          <Label for="edit-display">Display Name <span class="text-destructive">*</span></Label>
          <Input id="edit-display" bind:value={editDisplayName} required />
        </div>
        <div class="grid grid-cols-2 gap-2">
          <div class="space-y-1">
            <Label for="edit-unit">Unit identifier</Label>
            <Input id="edit-unit" bind:value={editUnit} />
          </div>
          <div class="space-y-1">
            <Label for="edit-symbol">Display symbol</Label>
            <Input id="edit-symbol" bind:value={editUnitSymbol} />
          </div>
        </div>
        <div class="space-y-1">
          <Label for="edit-type">Value type</Label>
          <select
            id="edit-type"
            bind:value={editValueType}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="numeric">numeric</option>
            <option value="range">range</option>
            <option value="text">text</option>
            <option value="enum">enum</option>
          </select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { editOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={editing}>
            {editing ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listParts, createPart, addListing } from '$lib/api/parts.js';
  import { listDistributorConfigs, searchDistributor } from '$lib/api/distributors.js';
  import type { DistributorSearchProduct } from '$lib/api/distributors.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import Pagination from '$lib/components/common/Pagination.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import StockBadge from '$lib/components/inventory/StockBadge.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ApiError } from '$lib/api/client.js';
  import { Plus } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();
  let q = $state('');
  let stockStatus = $state('');
  let limit = $state(50);
  let offset = $state(0);
  let createOpen = $state(false);
  let createMode = $state<'manual' | 'import'>('manual');
  let duplicateFound = $state<{ id: string; mpn: string | null; manufacturer: string | null; description: string | null } | null>(null);

  // Create part — manual mode
  let newMpn = $state('');
  let newManufacturer = $state('');
  let newDescription = $state('');
  let newScope = $state<'global' | 'team'>('team');
  let creating = $state(false);

  // Create part — import from distributor mode
  let importDistributorId = $state('');
  let importKeywords = $state('');
  let importResults = $state<DistributorSearchProduct[]>([]);
  let importSearching = $state(false);
  let importSelected = $state<DistributorSearchProduct | null>(null);
  let importScope = $state<'global' | 'team'>('team');
  let importing = $state(false);

  let configsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
    enabled: createOpen && createMode === 'import',
  }));

  const enabledConfigs = $derived((configsQuery.data?.items ?? []).filter((c) => c.enabled));

  // Auto-select preferred distributor.
  $effect(() => {
    if (createMode === 'import' && enabledConfigs.length > 0 && !importDistributorId) {
      const preferred = enabledConfigs.find((c) => c.preferred);
      importDistributorId = (preferred ?? enabledConfigs[0]).distributor_id;
    }
  });

  function resetDialog() {
    newMpn = ''; newManufacturer = ''; newDescription = ''; newScope = 'team';
    importKeywords = ''; importResults = []; importSelected = null; importDistributorId = ''; importScope = 'team';
    duplicateFound = null;
  }

  async function doImportSearch() {
    if (!importKeywords.trim() || !importDistributorId) return;
    importSelected = null;
    importSearching = true;
    try {
      const res = await searchDistributor(teamId, importDistributorId, importKeywords.trim());
      importResults = res.items;
    } catch (e: any) {
      pushToast('error', 'Search failed', e.message);
      importResults = [];
    } finally {
      importSearching = false;
    }
  }

  function onImportKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); doImportSearch(); }
  }

  let query = createQuery(() => ({
    queryKey: ['parts', teamId, q, stockStatus, limit, offset],
    queryFn: () => listParts({
      team_id: teamId,
      q: q || undefined,
      stock_status: stockStatus || undefined,
      limit,
      offset,
    }),
  }));

  function handleSearch(v: string) {
    q = v;
    offset = 0;
  }

  function handleStockFilter(e: Event) {
    stockStatus = (e.target as HTMLSelectElement).value;
    offset = 0;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    creating = true;
    duplicateFound = null;
    try {
      await createPart({
        scope: newScope,
        team_id: newScope === 'team' ? teamId : undefined,
        mpn: newMpn.trim() || undefined,
        manufacturer: newManufacturer.trim() || undefined,
        description: newDescription.trim() || undefined,
      });
      createOpen = false;
      resetDialog();
      qc.invalidateQueries({ queryKey: ['parts', teamId] });
      pushToast('success', 'Part created');
    } catch (err: any) {
      if (err instanceof ApiError && err.code === 'PART_EXISTS') {
        duplicateFound = (err.data as any).existing_part;
      } else {
        pushToast('error', 'Failed to create part', err.message);
      }
    } finally {
      creating = false;
    }
  }

  async function handleImport(e: Event) {
    e.preventDefault();
    if (!importSelected) return;
    importing = true;
    try {
      let partId: string;
      let isExisting = false;
      try {
        const part = await createPart({
          scope: importScope,
          team_id: importScope === 'team' ? teamId : undefined,
          mpn: importSelected.mpn || undefined,
          manufacturer: importSelected.manufacturer || undefined,
          description: importSelected.description || undefined,
          category: importSelected.category || undefined,
          package: importSelected.package || undefined,
        });
        partId = part.id;
      } catch (err: any) {
        if (err instanceof ApiError && err.code === 'PART_EXISTS') {
          partId = (err.data as any).existing_part.id;
          isExisting = true;
        } else {
          throw err;
        }
      }
      await addListing(partId, {
        sku: importSelected.primary_sku,
        distributor_id: importDistributorId,
        team_id: teamId,
      });
      createOpen = false;
      resetDialog();
      if (isExisting) {
        pushToast('success', 'Listing added to existing part', 'Parameters will populate shortly.');
      } else {
        pushToast('success', 'Part imported', 'Parameters will populate shortly.');
      }
      navigate(`/teams/${teamId}/parts/${partId}`);
    } catch (err: any) {
      pushToast('error', 'Failed to import part', err.message);
    } finally {
      importing = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Parts">
    {#snippet actions()}
      <Button size="sm" onclick={() => (createOpen = true)}>
        <Plus class="h-4 w-4 mr-1" /> Add Part
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <SearchInput placeholder="Search by MPN, description, or manufacturer…" onchange={handleSearch} />
      <select
        value={stockStatus}
        onchange={handleStockFilter}
        class="flex h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <option value="">All parts</option>
        <option value="in_stock">In stock</option>
        <option value="low_stock">Low stock</option>
        <option value="out_of_stock">Out of stock</option>
        <option value="not_tracked">Not tracked</option>
      </select>
    </div>

    {#if query.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if query.isError}
      <div class="text-sm text-destructive">Error: {query.error.message}</div>
    {:else if query.data}
      {@const data = query.data}
      {#if data.items.length === 0}
        <EmptyState
          title="No parts found"
          description={q || stockStatus ? 'Try adjusting your filters.' : 'Add your first part to get started.'}
        />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>MPN</TableHead>
              <TableHead>Manufacturer</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Category</TableHead>
              <TableHead>Status</TableHead>
              <TableHead class="text-right">Qty</TableHead>
              <TableHead>Stock</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each data.items as part (part.id)}
              <TableRow
                class="cursor-pointer"
                onclick={() => navigate(`/teams/${teamId}/parts/${part.id}`)}
              >
                <TableCell class="font-mono font-medium text-xs">{part.mpn ?? '—'}</TableCell>
                <TableCell>{part.manufacturer ?? '—'}</TableCell>
                <TableCell class="max-w-xs truncate text-muted-foreground">{part.description ?? '—'}</TableCell>
                <TableCell>{part.category ?? '—'}</TableCell>
                <TableCell><LifecycleBadge status={part.lifecycle_status} /></TableCell>
                <TableCell class="text-right font-medium tabular-nums">
                  {part.inventory_entry_id != null ? (part.quantity_on_hand ?? 0) : '—'}
                </TableCell>
                <TableCell>
                  {#if part.inventory_entry_id != null}
                    <StockBadge
                      quantity_on_hand={part.quantity_on_hand ?? 0}
                      minimum_stock_quantity={part.minimum_stock_quantity}
                    />
                  {:else}
                    <span class="text-xs text-muted-foreground">Not tracked</span>
                  {/if}
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
        <Pagination
          total={data.total}
          {limit}
          {offset}
          onchange={(o) => (offset = o)}
        />
      {/if}
    {/if}
  </div>
</div>

<!-- Create Part Dialog -->
<DialogPrimitive.Root bind:open={createOpen} onOpenChange={(o) => { if (!o) resetDialog(); }}>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <DialogPrimitive.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <DialogPrimitive.Title class="text-lg font-semibold mb-4">Add Part</DialogPrimitive.Title>

      <!-- Mode toggle -->
      <div class="flex gap-1 mb-4 border border-border rounded-md p-0.5 w-fit">
        <button
          type="button"
          class="px-3 py-1 text-sm rounded transition-colors {createMode === 'manual' ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'}"
          onclick={() => { createMode = 'manual'; importResults = []; importSelected = null; }}
        >Create manually</button>
        <button
          type="button"
          class="px-3 py-1 text-sm rounded transition-colors {createMode === 'import' ? 'bg-primary text-primary-foreground' : 'hover:bg-accent'}"
          onclick={() => { createMode = 'import'; }}
        >Import from distributor</button>
      </div>

      {#if createMode === 'manual'}
        <form onsubmit={handleCreate} class="space-y-3">
          <div class="space-y-1">
            <Label for="scope">Scope</Label>
            <Select id="scope" bind:value={newScope}>
              <option value="team">Team-scoped</option>
              <option value="global">Global</option>
            </Select>
          </div>
          <div class="space-y-1">
            <Label for="mpn">MPN</Label>
            <Input id="mpn" bind:value={newMpn} placeholder="e.g. LM317T" oninput={() => (duplicateFound = null)} />
          </div>
          <div class="space-y-1">
            <Label for="mfr">Manufacturer</Label>
            <Input id="mfr" bind:value={newManufacturer} placeholder="e.g. Texas Instruments" oninput={() => (duplicateFound = null)} />
          </div>
          <div class="space-y-1">
            <Label for="desc">Description</Label>
            <Input id="desc" bind:value={newDescription} placeholder="Short description" />
          </div>
          {#if duplicateFound}
            <div class="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-700 dark:bg-amber-950">
              <p class="font-medium text-amber-800 dark:text-amber-200">This part already exists</p>
              <p class="mt-0.5 text-amber-700 dark:text-amber-300">
                {duplicateFound.mpn ?? ''}
                {#if duplicateFound.manufacturer} · {duplicateFound.manufacturer}{/if}
                {#if duplicateFound.description} — {duplicateFound.description}{/if}
              </p>
              <Button
                size="sm"
                class="mt-2"
                onclick={() => { createOpen = false; resetDialog(); navigate(`/teams/${teamId}/parts/${duplicateFound!.id}`); }}
              >
                Open existing part
              </Button>
            </div>
          {/if}
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onclick={() => (createOpen = false)}>Cancel</Button>
            <Button type="submit" disabled={creating || !!duplicateFound}>{creating ? 'Creating…' : 'Create'}</Button>
          </div>
        </form>

      {:else}
        <form onsubmit={handleImport} class="space-y-3">
          <div class="space-y-1">
            <Label>Scope</Label>
            <Select bind:value={importScope}>
              <option value="team">Team-scoped</option>
              <option value="global">Global</option>
            </Select>
          </div>
          <div class="space-y-1">
            <Label>Distributor</Label>
            <Select bind:value={importDistributorId} onchange={() => { importKeywords = ''; importResults = []; importSelected = null; }}>
              {#each enabledConfigs as cfg (cfg.distributor_id)}
                <option value={cfg.distributor_id}>{cfg.distributor_name}{cfg.preferred ? ' (preferred)' : ''}</option>
              {/each}
            </Select>
          </div>
          <div class="space-y-1">
            <Label>Search</Label>
            <div class="flex gap-2">
              <Input
                placeholder="MPN, description…"
                bind:value={importKeywords}
                onkeydown={onImportKeydown}
                disabled={!importDistributorId}
                class="flex-1"
              />
              <Button type="button" variant="outline" size="sm" disabled={!importKeywords.trim() || importSearching} onclick={doImportSearch}>
                {importSearching ? 'Searching…' : 'Search'}
              </Button>
            </div>
          </div>

          {#if importResults.length > 0}
            <div class="border border-border rounded-md divide-y divide-border max-h-48 overflow-y-auto">
              {#each importResults as result (result.primary_sku)}
                {@const isSelected = importSelected?.primary_sku === result.primary_sku}
                <button
                  type="button"
                  class="w-full text-left px-3 py-2 hover:bg-accent/50 transition-colors {isSelected ? 'bg-accent' : ''}"
                  onclick={() => (importSelected = result)}
                >
                  <div class="flex items-center justify-between gap-2">
                    <div class="min-w-0">
                      <p class="font-mono text-xs font-medium truncate">{result.mpn}</p>
                      <p class="text-xs text-muted-foreground truncate">{result.manufacturer} · {result.description}</p>
                    </div>
                    <p class="shrink-0 text-xs font-mono text-muted-foreground">{result.primary_sku}</p>
                  </div>
                </button>
              {/each}
            </div>
          {/if}

          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onclick={() => (createOpen = false)}>Cancel</Button>
            <Button type="submit" disabled={!importSelected || importing}>
              {importing ? 'Importing…' : 'Import'}
            </Button>
          </div>
        </form>
      {/if}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

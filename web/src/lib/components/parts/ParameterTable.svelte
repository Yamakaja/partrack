<script lang="ts">
  import type { Parameter, Listing } from '$lib/types/parts.js';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { createParameter, deleteParameter } from '$lib/api/parts.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus, Trash2, Pencil } from 'lucide-svelte';

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

  let addingParam = $state(false);
  let newName = $state('');
  let newValue = $state('');
  let newUnit = $state('');

  // Which tab triggered the add form (so we show it in the right place)
  let addingFrom = $state<'user' | 'combined'>('user');

  // Override state: when set, pre-fills the add form for the combined tab
  let overrideParam = $state<{ name: string; value: string; unit: string } | null>(null);

  function startOverride(param: Parameter & { displaySource: string }) {
    overrideParam = { name: param.name, value: param.value, unit: param.unit ?? '' };
    newName = param.name;
    newValue = param.value;
    newUnit = param.unit ?? '';
    addingFrom = 'combined';
    addingParam = true;
  }

  function startAdd(from: 'user' | 'combined') {
    overrideParam = null;
    newName = '';
    newValue = '';
    newUnit = '';
    addingFrom = from;
    addingParam = true;
  }

  function cancelAdd() {
    addingParam = false;
    overrideParam = null;
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

  async function removeParam(paramId: string) {
    try {
      await deleteParameter(partId, paramId);
      onchange?.();
    } catch (e: any) {
      pushToast('error', 'Failed to delete parameter', e.message);
    }
  }

  // Combined parameter list with priority: user > preferred distributor > all distributors
  let combined = $derived.by(() => {
    const userNames = new Set((parameters.user_specified ?? []).map(p => p.name));
    const userRows = (parameters.user_specified ?? []).map(p => ({ ...p, displaySource: 'User' }));

    const sourced = parameters.distributor_sourced ?? [];
    let distRows: (Parameter & { displaySource: string })[];
    if (preferredSourceId) {
      distRows = sourced
        .filter(p => p.source_distributor_id === preferredSourceId && !userNames.has(p.name))
        .map(p => ({ ...p, displaySource: p.source_distributor_name ?? '' }));
    } else {
      distRows = sourced
        .filter(p => !userNames.has(p.name))
        .map(p => ({ ...p, displaySource: p.source_distributor_name ?? '' }));
    }
    return [...userRows, ...distRows];
  });

  function listingTabLabel(listing: Listing): string {
    const siblings = (listings ?? []).filter(l => l.distributor_id === listing.distributor_id);
    if (siblings.length <= 1) return listing.distributor_name;
    return `${listing.distributor_name} (${listing.packaging_type ?? listing.sku})`;
  }
</script>

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
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Value</TableHead>
          <TableHead>Unit</TableHead>
          <TableHead>Source</TableHead>
          <TableHead class="w-10"></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#each combined as param (param.id)}
          <TableRow>
            <TableCell class="font-mono text-xs">{param.name}</TableCell>
            <TableCell>{param.value}</TableCell>
            <TableCell class="text-muted-foreground">{param.unit ?? ''}</TableCell>
            <TableCell class="text-muted-foreground text-xs">{param.displaySource}</TableCell>
            <TableCell>
              {#if param.displaySource !== 'User'}
                <Button variant="ghost" size="icon" title="Override with user value" onclick={() => startOverride(param)}>
                  <Pencil class="h-3.5 w-3.5" />
                </Button>
              {/if}
            </TableCell>
          </TableRow>
        {:else}
          <TableRow>
            <TableCell colspan={5} class="text-center text-muted-foreground py-8">No parameters</TableCell>
          </TableRow>
        {/each}
        {#if addingParam && addingFrom === 'combined'}
          <TableRow>
            <TableCell>
              <input bind:value={newName} placeholder="name" class="w-full rounded border border-input bg-background px-2 py-1 text-xs" />
            </TableCell>
            <TableCell>
              <input bind:value={newValue} placeholder="value" class="w-full rounded border border-input bg-background px-2 py-1 text-xs" />
            </TableCell>
            <TableCell>
              <input bind:value={newUnit} placeholder="unit" class="w-full rounded border border-input bg-background px-2 py-1 text-xs" />
            </TableCell>
            <TableCell class="text-muted-foreground text-xs">User</TableCell>
            <TableCell>
              <Button size="sm" onclick={addParam}>Save</Button>
            </TableCell>
          </TableRow>
        {/if}
      </TableBody>
    </Table>
    <div class="mt-2">
      {#if !addingParam || addingFrom !== 'combined'}
        <Button variant="outline" size="sm" onclick={() => startAdd('combined')}>
          <Plus class="h-3.5 w-3.5 mr-1" /> Add parameter
        </Button>
      {:else}
        <Button variant="ghost" size="sm" onclick={cancelAdd}>Cancel</Button>
      {/if}
    </div>
  </TabsContent>

  <!-- User-specified parameters tab -->
  <TabsContent value="user">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Value</TableHead>
          <TableHead>Unit</TableHead>
          <TableHead class="w-10"></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#each (parameters.user_specified ?? []) as param (param.id)}
          <TableRow>
            <TableCell class="font-mono text-xs">{param.name}</TableCell>
            <TableCell>{param.value}</TableCell>
            <TableCell class="text-muted-foreground">{param.unit ?? ''}</TableCell>
            <TableCell>
              <Button variant="ghost" size="icon" onclick={() => removeParam(param.id)}>
                <Trash2 class="h-3.5 w-3.5" />
              </Button>
            </TableCell>
          </TableRow>
        {/each}
        {#if addingParam && addingFrom === 'user'}
          <TableRow>
            <TableCell>
              <input bind:value={newName} placeholder="name" class="w-full rounded border border-input bg-background px-2 py-1 text-xs" />
            </TableCell>
            <TableCell>
              <input bind:value={newValue} placeholder="value" class="w-full rounded border border-input bg-background px-2 py-1 text-xs" />
            </TableCell>
            <TableCell>
              <input bind:value={newUnit} placeholder="unit" class="w-full rounded border border-input bg-background px-2 py-1 text-xs" />
            </TableCell>
            <TableCell>
              <Button size="sm" onclick={addParam}>Save</Button>
            </TableCell>
          </TableRow>
        {/if}
      </TableBody>
    </Table>
    <div class="mt-2">
      {#if !addingParam || addingFrom !== 'user'}
        <Button variant="outline" size="sm" onclick={() => startAdd('user')}>
          <Plus class="h-3.5 w-3.5 mr-1" /> Add parameter
        </Button>
      {:else}
        <Button variant="ghost" size="sm" onclick={cancelAdd}>Cancel</Button>
      {/if}
    </div>
  </TabsContent>

  <!-- Per-listing distributor parameter tabs -->
  {#each (listings ?? []) as listing (listing.id)}
    {@const distParams = (parameters.distributor_sourced ?? []).filter(p => p.source_distributor_id === listing.distributor_id)}
    <TabsContent value="dist-{listing.id}">
      <div class="flex gap-6 text-sm text-muted-foreground mb-3 px-1">
        <span>SKU: <span class="font-mono text-foreground">{listing.sku}</span></span>
        {#if listing.packaging_type}<span>Packaging: {listing.packaging_type}</span>{/if}
        {#if listing.unit_price != null}<span>Price: ${parseFloat(listing.unit_price).toFixed(4)}</span>{/if}
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Value</TableHead>
            <TableHead>Unit</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each distParams as param (param.id)}
            <TableRow>
              <TableCell class="font-mono text-xs">{param.name}</TableCell>
              <TableCell>{param.value}</TableCell>
              <TableCell class="text-muted-foreground">{param.unit ?? ''}</TableCell>
            </TableRow>
          {:else}
            <TableRow>
              <TableCell colspan={3} class="text-center text-muted-foreground py-8">No parameters from this distributor</TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
    </TabsContent>
  {/each}
</Tabs>

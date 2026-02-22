<script lang="ts">
  import type { Parameter } from '$lib/types/parts.js';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { createParameter, deleteParameter } from '$lib/api/parts.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus, Trash2 } from 'lucide-svelte';

  let {
    partId,
    parameters,
    onchange,
  }: {
    partId: string;
    parameters: { user_specified: Parameter[]; distributor_sourced: Parameter[] };
    onchange?: () => void;
  } = $props();

  let addingParam = $state(false);
  let newName = $state('');
  let newValue = $state('');
  let newUnit = $state('');

  async function addParam() {
    if (!newName.trim() || !newValue.trim()) return;
    try {
      await createParameter(partId, { name: newName.trim(), value: newValue.trim(), unit: newUnit.trim() || undefined });
      newName = ''; newValue = ''; newUnit = '';
      addingParam = false;
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
</script>

<Tabs value="user">
  <TabsList>
    <TabsTrigger value="user">User-specified ({parameters.user_specified?.length ?? 0})</TabsTrigger>
    <TabsTrigger value="distributor">Distributor ({parameters.distributor_sourced?.length ?? 0})</TabsTrigger>
  </TabsList>

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
        {#if addingParam}
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
      {#if !addingParam}
        <Button variant="outline" size="sm" onclick={() => (addingParam = true)}>
          <Plus class="h-3.5 w-3.5 mr-1" /> Add parameter
        </Button>
      {:else}
        <Button variant="ghost" size="sm" onclick={() => (addingParam = false)}>Cancel</Button>
      {/if}
    </div>
  </TabsContent>

  <TabsContent value="distributor">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Value</TableHead>
          <TableHead>Unit</TableHead>
          <TableHead>Source</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {#each (parameters.distributor_sourced ?? []) as param (param.id)}
          <TableRow>
            <TableCell class="font-mono text-xs">{param.name}</TableCell>
            <TableCell>{param.value}</TableCell>
            <TableCell class="text-muted-foreground">{param.unit ?? ''}</TableCell>
            <TableCell class="text-muted-foreground text-xs">{param.source_distributor_name ?? ''}</TableCell>
          </TableRow>
        {/each}
      </TableBody>
    </Table>
  </TabsContent>
</Tabs>

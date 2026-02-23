<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listParts, createPart } from '$lib/api/parts.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import SearchInput from '$lib/components/common/SearchInput.svelte';
  import Pagination from '$lib/components/common/Pagination.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();
  let q = $state('');
  let limit = $state(50);
  let offset = $state(0);
  let createOpen = $state(false);

  // Create part form
  let newMpn = $state('');
  let newManufacturer = $state('');
  let newDescription = $state('');
  let newScope = $state<'global' | 'team'>('team');
  let creating = $state(false);

  let query = createQuery(() => ({
    queryKey: ['parts', teamId, q, limit, offset],
    queryFn: () => listParts({ team_id: teamId, q: q || undefined, limit, offset }),
  }));

  function handleSearch(v: string) {
    q = v;
    offset = 0;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    creating = true;
    try {
      await createPart({
        scope: newScope,
        team_id: newScope === 'team' ? teamId : undefined,
        mpn: newMpn.trim() || undefined,
        manufacturer: newManufacturer.trim() || undefined,
        description: newDescription.trim() || undefined,
      });
      createOpen = false;
      newMpn = ''; newManufacturer = ''; newDescription = '';
      qc.invalidateQueries({ queryKey: ['parts', teamId] });
      pushToast('success', 'Part created');
    } catch (e: any) {
      pushToast('error', 'Failed to create part', e.message);
    } finally {
      creating = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Parts Catalog">
    {#snippet actions()}
      <Button size="sm" onclick={() => (createOpen = true)}>
        <Plus class="h-4 w-4 mr-1" /> Add Part
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6 space-y-4">
    <SearchInput placeholder="Search by MPN, description, or manufacturer…" onchange={handleSearch} />

    {#if query.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if query.isError}
      <div class="text-sm text-destructive">Error: {query.error.message}</div>
    {:else if query.data}
      {@const data = query.data}
      {#if data.items.length === 0}
        <EmptyState
          title="No parts found"
          description={q ? 'Try a different search term.' : 'Add your first part to get started.'}
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
              <TableHead>Scope</TableHead>
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
                <TableCell class="capitalize text-muted-foreground">{part.scope}</TableCell>
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
<DialogPrimitive.Root bind:open={createOpen}>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <DialogPrimitive.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <DialogPrimitive.Title class="text-lg font-semibold mb-4">Add Part</DialogPrimitive.Title>
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
          <Input id="mpn" bind:value={newMpn} placeholder="e.g. LM317T" />
        </div>
        <div class="space-y-1">
          <Label for="mfr">Manufacturer</Label>
          <Input id="mfr" bind:value={newManufacturer} placeholder="e.g. Texas Instruments" />
        </div>
        <div class="space-y-1">
          <Label for="desc">Description</Label>
          <Input id="desc" bind:value={newDescription} placeholder="Short description" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => (createOpen = false)}>Cancel</Button>
          <Button type="submit" disabled={creating}>{creating ? 'Creating…' : 'Create'}</Button>
        </div>
      </form>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

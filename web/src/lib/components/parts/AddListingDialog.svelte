<script lang="ts">
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { createQuery } from '@tanstack/svelte-query';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { listDistributorConfigs, searchDistributor } from '$lib/api/distributors.js';
  import type { DistributorSearchProduct } from '$lib/api/distributors.js';
  import { addListing } from '$lib/api/parts.js';

  let {
    partId,
    teamId,
    open = $bindable(false),
    onadded,
  }: {
    partId: string;
    teamId: string;
    open?: boolean;
    onadded?: () => void;
  } = $props();

  let selectedDistributorId = $state('');
  let keywords = $state('');
  let searchResults = $state<DistributorSearchProduct[]>([]);
  let searching = $state(false);
  let selectedResult = $state<DistributorSearchProduct | null>(null);
  let adding = $state(false);

  let configsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
    enabled: open,
  }));

  const enabledConfigs = $derived((configsQuery.data?.items ?? []).filter((c) => c.enabled));

  // Auto-select the preferred distributor when configs load.
  $effect(() => {
    if (enabledConfigs.length > 0 && !selectedDistributorId) {
      const preferred = enabledConfigs.find((c) => c.preferred);
      selectedDistributorId = (preferred ?? enabledConfigs[0]).distributor_id;
    }
  });

  // Reset state when dialog opens.
  $effect(() => {
    if (open) {
      keywords = '';
      searchResults = [];
      selectedResult = null;
    }
  });

  async function doSearch() {
    if (!keywords.trim() || !selectedDistributorId) return;
    selectedResult = null;
    searching = true;
    try {
      const res = await searchDistributor(teamId, selectedDistributorId, keywords.trim());
      searchResults = res.items;
    } catch (e: any) {
      pushToast('error', 'Search failed', e.message);
      searchResults = [];
    } finally {
      searching = false;
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); doSearch(); }
  }

  async function confirm() {
    if (!selectedResult) return;
    adding = true;
    try {
      await addListing(partId, {
        sku: selectedResult.primary_sku,
        distributor_id: selectedDistributorId,
        team_id: teamId,
      });
      pushToast('info', 'Listing added', 'Parameters will populate shortly.');
      open = false;
      onadded?.();
    } catch (e: any) {
      pushToast('error', 'Failed to add listing', e.message);
    } finally {
      adding = false;
    }
  }
</script>

<DialogPrimitive.Root bind:open>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <DialogPrimitive.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <DialogPrimitive.Title class="text-lg font-semibold mb-4">Add Distributor Listing</DialogPrimitive.Title>

      <div class="space-y-3">
        <div class="space-y-1">
          <Label>Distributor</Label>
          <Select bind:value={selectedDistributorId} onchange={() => { keywords = ''; searchResults = []; selectedResult = null; }}>
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
              bind:value={keywords}
              onkeydown={onKeydown}
              disabled={!selectedDistributorId}
              class="flex-1"
            />
            <Button type="button" variant="outline" size="sm" disabled={!keywords.trim() || searching} onclick={doSearch}>
              {searching ? 'Searching…' : 'Search'}
            </Button>
          </div>
        </div>

        {#if searchResults.length > 0}
          <div class="border border-border rounded-md divide-y divide-border max-h-64 overflow-y-auto">
            {#each searchResults as result (result.primary_sku)}
              {@const isSelected = selectedResult?.primary_sku === result.primary_sku}
              <button
                type="button"
                class="w-full text-left px-3 py-2 hover:bg-accent/50 transition-colors {isSelected ? 'bg-accent' : ''}"
                onclick={() => (selectedResult = result)}
              >
                <div class="flex items-center justify-between gap-2">
                  <div class="min-w-0">
                    <p class="font-mono text-xs font-medium truncate">{result.mpn}</p>
                    <p class="text-xs text-muted-foreground truncate">{result.manufacturer} · {result.description}</p>
                  </div>
                  <div class="shrink-0 text-right">
                    <p class="text-xs font-mono">{result.unit_price ? `$${parseFloat(result.unit_price).toFixed(4)}` : '—'}</p>
                    <p class="text-xs text-muted-foreground font-mono">{result.primary_sku}</p>
                  </div>
                </div>
              </button>
            {/each}
          </div>
        {/if}

        {#if selectedResult}
          <div class="rounded-md bg-accent/30 border border-border px-3 py-2 text-sm">
            <p class="font-medium">Selected: <span class="font-mono">{selectedResult.primary_sku}</span></p>
            <p class="text-muted-foreground">{selectedResult.mpn} · {selectedResult.manufacturer}</p>
          </div>
        {/if}
      </div>

      <div class="flex justify-end gap-2 mt-6">
        <DialogPrimitive.Close>
          <Button variant="outline" type="button">Cancel</Button>
        </DialogPrimitive.Close>
        <Button disabled={!selectedResult || adding} onclick={confirm}>
          {adding ? 'Adding…' : 'Add Listing'}
        </Button>
      </div>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

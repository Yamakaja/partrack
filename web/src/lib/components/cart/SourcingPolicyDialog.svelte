<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getSourcingPolicy, updateSourcingPolicy, type SourcingPolicyDistributor } from '$lib/api/sourcing.js';
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ArrowUp, ArrowDown, X, Plus } from 'lucide-svelte';

  let { teamId, open = $bindable() }: { teamId: string; open: boolean } = $props();

  const qc = useQueryClient();

  let policyQuery = createQuery(() => ({
    queryKey: ['sourcing-policy', teamId],
    queryFn: () => getSourcingPolicy(teamId),
    enabled: open,
  }));

  // Editable local state.
  let strategy = $state<'cost' | 'preferred_in_stock'>('cost');
  let tolPct = $state('');
  let tolAbs = $state('');
  let ranked = $state<SourcingPolicyDistributor[]>([]);
  let unranked = $state<SourcingPolicyDistributor[]>([]);
  let saving = $state(false);
  let loadedFor = $state<string | null>(null);

  // Seed local state from the fetched policy the first time it loads per open.
  $effect(() => {
    if (!open) { loadedFor = null; return; }
    const data = policyQuery.data;
    if (!data || loadedFor === teamId) return;
    strategy = data.strategy;
    tolPct = data.cost_tolerance_pct ?? '';
    tolAbs = data.cost_tolerance_abs ?? '';
    const enabled = data.distributors.filter((d) => d.enabled);
    ranked = enabled
      .filter((d) => d.priority != null)
      .sort((a, b) => (a.priority ?? 0) - (b.priority ?? 0));
    unranked = enabled.filter((d) => d.priority == null);
    loadedFor = teamId;
  });

  function moveUp(i: number) {
    if (i <= 0) return;
    const a = [...ranked];
    [a[i - 1], a[i]] = [a[i], a[i - 1]];
    ranked = a;
  }
  function moveDown(i: number) {
    if (i >= ranked.length - 1) return;
    const a = [...ranked];
    [a[i + 1], a[i]] = [a[i], a[i + 1]];
    ranked = a;
  }
  function removeRank(i: number) {
    const d = ranked[i];
    ranked = ranked.filter((_, j) => j !== i);
    unranked = [...unranked, d].sort((a, b) => a.distributor_name.localeCompare(b.distributor_name));
  }
  function addRank(d: SourcingPolicyDistributor) {
    unranked = unranked.filter((x) => x.distributor_id !== d.distributor_id);
    ranked = [...ranked, d];
  }

  async function save() {
    const pct = tolPct.trim() === '' ? null : parseFloat(tolPct);
    const abs = tolAbs.trim() === '' ? null : parseFloat(tolAbs);
    if ((pct != null && (isNaN(pct) || pct < 0)) || (abs != null && (isNaN(abs) || abs < 0))) {
      pushToast('error', 'Invalid tolerance', 'Tolerances must be non-negative numbers.');
      return;
    }
    saving = true;
    try {
      const preferred = strategy === 'preferred_in_stock';
      await updateSourcingPolicy(teamId, {
        strategy,
        cost_tolerance_pct: preferred ? pct : null,
        cost_tolerance_abs: preferred ? abs : null,
        distributor_priority: preferred ? ranked.map((d) => d.distributor_id) : [],
      });
      qc.invalidateQueries({ queryKey: ['sourcing-policy', teamId] });
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      pushToast('success', 'Sourcing policy saved');
      open = false;
    } catch (err: any) {
      pushToast('error', 'Failed to save sourcing policy', err.message);
    } finally {
      saving = false;
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg max-h-[85vh] overflow-auto">
      <Dialog.Title class="text-lg font-semibold">Sourcing Policy</Dialog.Title>
      <Dialog.Description class="text-sm text-muted-foreground mb-4">
        Control how cart items are matched to a distributor listing.
      </Dialog.Description>

      {#if policyQuery.isPending}
        <div class="text-sm text-muted-foreground">Loading…</div>
      {:else if policyQuery.isError}
        <div class="text-sm text-destructive">Error: {policyQuery.error.message}</div>
      {:else}
        <div class="space-y-5">
          <!-- Strategy -->
          <div class="space-y-2">
            <Label>Strategy</Label>
            <div class="grid grid-cols-1 gap-2">
              <button
                type="button"
                class="text-left rounded-md border p-3 {strategy === 'cost' ? 'border-primary ring-1 ring-primary' : 'border-border'}"
                onclick={() => (strategy = 'cost')}
              >
                <div class="text-sm font-medium">Lowest cost</div>
                <div class="text-xs text-muted-foreground">Always pick the cheapest listing across your enabled distributors.</div>
              </button>
              <button
                type="button"
                class="text-left rounded-md border p-3 {strategy === 'preferred_in_stock' ? 'border-primary ring-1 ring-primary' : 'border-border'}"
                onclick={() => (strategy = 'preferred_in_stock')}
              >
                <div class="text-sm font-medium">Preferred distributor, in stock</div>
                <div class="text-xs text-muted-foreground">Order from your ranked distributors in order; fall to the next when one is out of stock.</div>
              </button>
            </div>
          </div>

          {#if strategy === 'preferred_in_stock'}
            <!-- Ranking -->
            <div class="space-y-2">
              <Label>Distributor ranking</Label>
              {#if ranked.length === 0}
                <p class="text-xs text-muted-foreground">Add distributors below to set your preference order.</p>
              {:else}
                <ol class="space-y-1">
                  {#each ranked as d, i (d.distributor_id)}
                    <li class="flex items-center gap-2 rounded-md border border-border px-2 py-1.5">
                      <span class="text-xs text-muted-foreground w-5 tabular-nums">{i + 1}.</span>
                      <span class="flex-1 text-sm">{d.distributor_name}</span>
                      <Button variant="ghost" size="icon" class="h-6 w-6" disabled={i === 0} onclick={() => moveUp(i)} title="Move up">
                        <ArrowUp class="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="icon" class="h-6 w-6" disabled={i === ranked.length - 1} onclick={() => moveDown(i)} title="Move down">
                        <ArrowDown class="h-3.5 w-3.5" />
                      </Button>
                      <Button variant="ghost" size="icon" class="h-6 w-6 text-muted-foreground" onclick={() => removeRank(i)} title="Remove from ranking">
                        <X class="h-3.5 w-3.5" />
                      </Button>
                    </li>
                  {/each}
                </ol>
              {/if}

              {#if unranked.length > 0}
                <div class="flex flex-wrap gap-1.5 pt-1">
                  {#each unranked as d (d.distributor_id)}
                    <Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => addRank(d)}>
                      <Plus class="h-3 w-3 mr-1" />{d.distributor_name}
                    </Button>
                  {/each}
                </div>
              {/if}
              <p class="text-xs text-muted-foreground">
                Unranked enabled distributors are still used as a last resort when no ranked distributor has stock.
              </p>
            </div>

            <!-- Cost guardrail -->
            <div class="space-y-2">
              <Label>Cost guardrail <span class="text-muted-foreground font-normal">(optional)</span></Label>
              <p class="text-xs text-muted-foreground">
                Only honour the preferred distributor when it's within this much of the cheapest option. Leave blank to prefer at any price.
              </p>
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1">
                  <Label for="tol-pct" class="text-xs font-normal">Within percent (%)</Label>
                  <Input id="tol-pct" type="number" min="0" step="0.5" placeholder="e.g. 15" bind:value={tolPct} />
                </div>
                <div class="space-y-1">
                  <Label for="tol-abs" class="text-xs font-normal">Within amount (abs)</Label>
                  <Input id="tol-abs" type="number" min="0" step="0.01" placeholder="e.g. 5.00" bind:value={tolAbs} />
                </div>
              </div>
            </div>
          {/if}

          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onclick={() => (open = false)}>Cancel</Button>
            <Button type="button" disabled={saving} onclick={save}>{saving ? 'Saving…' : 'Save'}</Button>
          </div>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

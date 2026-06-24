<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import {
    createPartResolveItem,
    getPartResolveItem,
    commitResolveItem,
    retryResolveItemDistributors,
  } from '$lib/api/partResolve.js';
  import type { BulkScanTaskType } from '$lib/api/bulkScan.js';
  import { createPart, getPart } from '$lib/api/parts.js';
  import { createInventoryEntry, createAdjustment } from '$lib/api/inventory.js';
  import { listStorageLocations } from '$lib/api/storage.js';
  import { listDistributorConfigs } from '$lib/api/distributors.js';
  import { ApiError } from '$lib/api/client.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import StorageLocationPicker, { type StorageLocationValue } from '$lib/components/storage/StorageLocationPicker.svelte';
  import AdjustStockForm from '$lib/components/inventory/AdjustStockForm.svelte';
  import ListingSelector from './ListingSelector.svelte';

  let {
    open = $bindable(false),
    teamId,
    onCommitted,
  }: {
    open?: boolean;
    teamId: string;
    onCommitted?: (partId: string) => void;
  } = $props();

  type Step = 'entry' | 'resolving' | 'review' | 'manual' | 'done';
  let step = $state<Step>('entry');
  let submitting = $state(false);
  let submitError = $state('');
  let doneMessage = $state('');

  // ── Entry fields ────────────────────────────────────────────────────────
  let entryDistId = $state('');
  let entrySku = $state('');
  let entryMpn = $state('');
  let entryManufacturer = $state('');

  let resolveItemId = $state<string | null>(null);

  // ── Unknown-part import selection ─────────────────────────────────────────
  let primaryListingKey = $state('');
  let includedListings = $state<Set<string>>(new Set());
  let importInitialQty = $state('');
  let importInitialReason = $state('initial_count');
  let importStorageLoc = $state<StorageLocationValue | null>(null);

  // ── Known-part create-entry ───────────────────────────────────────────────
  let createQty = $state('');
  let createStorageLoc = $state<StorageLocationValue | null>(null);

  // ── Known-part stock adjustment ───────────────────────────────────────────
  let adjustMode = $state<'delta' | 'set'>('delta');
  let adjustQty = $state('');
  let adjustReason = $state('received');

  // ── Manual metadata ───────────────────────────────────────────────────────
  let manScope = $state<'global' | 'team'>('team');
  let manMpn = $state('');
  let manManufacturer = $state('');
  let manDescription = $state('');
  let manQty = $state('');
  let manStorageLoc = $state<StorageLocationValue | null>(null);
  let duplicateFound = $state<{ id: string; mpn: string | null; manufacturer: string | null; description: string | null } | null>(null);

  function parseListingKey(key: string): { distId: string; sku: string } {
    return { distId: key.slice(0, 36), sku: key.slice(37) };
  }

  // ── Remote data ─────────────────────────────────────────────────────────
  const distributorConfigsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
    staleTime: 60_000,
  }));
  const enabledConfigs = $derived(distributorConfigsQuery.data?.items.filter((c) => c.enabled) ?? []);
  const preferredDistId = $derived(enabledConfigs.find((c) => c.preferred)?.distributor_id ?? null);

  const locationsQuery = createQuery(() => ({
    queryKey: ['storage', teamId],
    queryFn: () => listStorageLocations(teamId),
    staleTime: 60_000,
  }));
  const locations = $derived(locationsQuery.data?.items ?? []);

  const resolveQuery = createQuery(() => ({
    queryKey: ['part-resolve', teamId, resolveItemId],
    queryFn: () => getPartResolveItem(teamId, resolveItemId!),
    enabled: !!resolveItemId && (step === 'resolving' || step === 'review'),
    refetchInterval: (q: { state: { data?: { status?: string } } }) =>
      q.state.data?.status === 'pending' ? 1000 : false,
  }));

  const resolve = $derived(resolveQuery.data ?? null);
  const taskType = $derived<BulkScanTaskType | null>(resolve?.task_type ?? null);
  const enrichment = $derived(resolve?.enrichment_data ?? null);
  const resolvedPartId = $derived(resolve?.resolved_part_id ?? null);

  const activePartQuery = createQuery(() => ({
    queryKey: ['part', resolvedPartId, teamId],
    queryFn: () => getPart(resolvedPartId!, teamId),
    enabled:
      !!resolvedPartId &&
      (taskType === 'known_part_create_entry' || taskType === 'known_part_stock_adjustment'),
  }));
  const activePart = $derived(activePartQuery.data ?? null);

  // ── Validation ────────────────────────────────────────────────────────────
  const hasSku = $derived(entrySku.trim() !== '');
  const hasMpn = $derived(entryMpn.trim() !== '');
  const canResolve = $derived((hasSku && entryDistId !== '') || hasMpn);

  // ── Step transition once resolution lands ─────────────────────────────────
  $effect(() => {
    if (step !== 'resolving' || !resolve) return;
    if (resolve.status === 'enriched' || resolve.status === 'failed') {
      const tt = resolve.task_type;
      if (resolve.status === 'failed' || tt === 'unrecognized' || (tt === 'unknown_part_import' && !resolve.enrichment_data)) {
        manMpn = entryMpn.trim();
        manManufacturer = entryManufacturer.trim();
        step = 'manual';
      } else {
        step = 'review';
      }
    }
  });

  // ── State resets ────────────────────────────────────────────────────────
  function resetSelectionState() {
    submitError = '';
    primaryListingKey = '';
    includedListings = new Set();
    importInitialQty = '';
    importInitialReason = 'initial_count';
    importStorageLoc = null;
    createQty = '';
    createStorageLoc = null;
    adjustMode = 'delta';
    adjustQty = '';
    adjustReason = 'received';
    duplicateFound = null;
    manMpn = '';
    manManufacturer = '';
    manDescription = '';
    manQty = '';
    manStorageLoc = null;
  }

  function resetForAnother() {
    resolveItemId = null;
    entryDistId = '';
    entrySku = '';
    entryMpn = '';
    entryManufacturer = '';
    resetSelectionState();
    doneMessage = '';
    step = 'entry';
  }

  function onOpenChange(o: boolean) {
    open = o;
    if (!o) resetForAnother();
  }

  // ── Actions ───────────────────────────────────────────────────────────────
  async function doResolve() {
    if (!canResolve || submitting) return;
    submitting = true;
    submitError = '';
    resetSelectionState();
    try {
      const { resolve_item_id } = await createPartResolveItem(teamId, {
        sku: hasSku ? entrySku.trim() : undefined,
        distributor_id: hasSku ? entryDistId : undefined,
        mpn: hasMpn ? entryMpn.trim() : undefined,
        manufacturer: entryManufacturer.trim() || undefined,
      });
      resolveItemId = resolve_item_id;
      step = 'resolving';
    } catch (e: unknown) {
      submitError = e instanceof Error ? e.message : 'Failed to start lookup';
    } finally {
      submitting = false;
    }
  }

  function finishDone(msg: string, partId: string) {
    doneMessage = msg;
    onCommitted?.(partId);
    step = 'done';
  }

  async function doCommit() {
    submitting = true;
    submitError = '';
    try {
      if (taskType === 'unknown_part_import') {
        if (!resolveItemId) throw new Error('No resolve item');
        if (!primaryListingKey) throw new Error('Select a primary listing');
        const { distId, sku } = parseListingKey(primaryListingKey);
        const body: Parameters<typeof commitResolveItem>[2] = {
          primary_distributor_id: distId,
          primary_sku: sku,
          additional_listings: [...includedListings].map((key) => {
            const p = parseListingKey(key);
            return { distributor_id: p.distId, sku: p.sku };
          }),
        };
        const qty = parseInt(importInitialQty, 10);
        if (!isNaN(qty) && qty !== 0) {
          body.initial_qty = qty;
          body.initial_qty_reason = importInitialReason;
        }
        if (importStorageLoc?.locationId) body.storage_location_id = importStorageLoc.locationId;
        const { part_id } = await commitResolveItem(teamId, resolveItemId, body);
        finishDone('Part imported.', part_id);

      } else if (taskType === 'known_part_create_entry') {
        if (!resolvedPartId) throw new Error('No part linked');
        const entry = await createInventoryEntry(teamId, {
          part_id: resolvedPartId,
          storage_location_id: createStorageLoc?.locationId ?? undefined,
          storage_coordinate: createStorageLoc?.coordinate ?? undefined,
        });
        const qty = parseInt(createQty, 10);
        if (!isNaN(qty) && qty > 0) {
          await createAdjustment(teamId, entry.id, { delta: qty, reason: 'initial_stock' });
        }
        finishDone('Inventory entry created.', resolvedPartId);

      } else if (taskType === 'known_part_stock_adjustment') {
        if (!activePart?.inventory_entry_id) throw new Error('No inventory entry found');
        const n = parseInt(adjustQty, 10);
        if (isNaN(n)) throw new Error('Enter a valid quantity');
        const delta = adjustMode === 'delta' ? n : n - (activePart.quantity_on_hand ?? 0);
        if (delta === 0) throw new Error('No change');
        await createAdjustment(teamId, activePart.inventory_entry_id, { delta, reason: adjustReason });
        finishDone('Stock adjusted.', activePart.id);
      }
    } catch (e: unknown) {
      submitError = e instanceof Error ? e.message : 'Commit failed';
    } finally {
      submitting = false;
    }
  }

  async function doManualCreate() {
    submitting = true;
    submitError = '';
    duplicateFound = null;
    try {
      const part = await createPart({
        scope: manScope,
        team_id: manScope === 'team' ? teamId : undefined,
        mpn: manMpn.trim() || undefined,
        manufacturer: manManufacturer.trim() || undefined,
        description: manDescription.trim() || undefined,
      });
      // Optional initial stock (team-scoped only).
      if (manScope === 'team') {
        const qty = parseInt(manQty, 10);
        if ((!isNaN(qty) && qty > 0) || manStorageLoc?.locationId) {
          const entry = await createInventoryEntry(teamId, {
            part_id: part.id,
            storage_location_id: manStorageLoc?.locationId ?? undefined,
            storage_coordinate: manStorageLoc?.coordinate ?? undefined,
          });
          if (!isNaN(qty) && qty > 0) {
            await createAdjustment(teamId, entry.id, { delta: qty, reason: 'initial_stock' });
          }
        }
      }
      finishDone('Part created.', part.id);
    } catch (e: unknown) {
      if (e instanceof ApiError && e.code === 'PART_EXISTS') {
        duplicateFound = (e.data as { existing_part: typeof duplicateFound }).existing_part;
      } else {
        submitError = e instanceof Error ? e.message : 'Failed to create part';
      }
    } finally {
      submitting = false;
    }
  }

  async function handleRetryDistributor(distributorId: string) {
    if (!resolveItemId) return;
    try {
      primaryListingKey = '';
      includedListings = new Set();
      step = 'resolving';
      await retryResolveItemDistributors(teamId, resolveItemId, [distributorId]);
      resolveQuery.refetch();
    } catch (e: unknown) {
      submitError = e instanceof Error ? e.message : 'Retry failed';
    }
  }

  function goManual() {
    manMpn = entryMpn.trim();
    manManufacturer = entryManufacturer.trim();
    submitError = '';
    duplicateFound = null;
    step = 'manual';
  }

  const dialogTitle = $derived(
    step === 'manual' ? 'Create part manually'
      : step === 'done' ? 'Done'
      : 'Add part',
  );
</script>

<DialogPrimitive.Root bind:open {onOpenChange}>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <DialogPrimitive.Content
      class="fixed left-1/2 top-1/2 z-50 flex max-h-[85vh] w-full max-w-2xl -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
    >
      <DialogPrimitive.Title class="px-6 pt-6 pb-2 text-lg font-semibold">{dialogTitle}</DialogPrimitive.Title>

      <div class="flex-1 overflow-y-auto px-6 pb-2">
        {#if step === 'entry'}
          <p class="text-sm text-muted-foreground mb-4">
            Look up a part by distributor SKU, MPN, or both. We'll search your enabled distributors and let you review before adding.
          </p>
          <div class="space-y-4">
            <div class="grid grid-cols-[1fr_1.4fr] gap-3">
              <div class="space-y-1">
                <Label>Distributor</Label>
                <Select bind:value={entryDistId}>
                  <option value="">— Select —</option>
                  {#each enabledConfigs as cfg (cfg.distributor_id)}
                    <option value={cfg.distributor_id}>{cfg.distributor_name}{cfg.preferred ? ' (preferred)' : ''}</option>
                  {/each}
                </Select>
              </div>
              <div class="space-y-1">
                <Label for="entry-sku">SKU</Label>
                <Input id="entry-sku" bind:value={entrySku} placeholder="e.g. 497-11767-ND" />
              </div>
            </div>
            <div class="relative flex items-center gap-3 text-xs text-muted-foreground">
              <span class="h-px flex-1 bg-border"></span>and / or<span class="h-px flex-1 bg-border"></span>
            </div>
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label for="entry-mpn">MPN</Label>
                <Input id="entry-mpn" bind:value={entryMpn} placeholder="e.g. STM32F407VGT6" />
              </div>
              <div class="space-y-1">
                <Label for="entry-mfr">Manufacturer</Label>
                <Input id="entry-mfr" bind:value={entryManufacturer} placeholder="optional" />
              </div>
            </div>
            {#if hasSku && entryDistId === ''}
              <p class="text-xs text-warning">Choose a distributor for the SKU, or use the MPN field instead.</p>
            {/if}
          </div>

        {:else if step === 'resolving'}
          <div class="flex items-center gap-3 py-10 text-muted-foreground">
            <div class="h-5 w-5 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
            <p class="text-sm">Looking up distributors…</p>
          </div>

        {:else if step === 'review' && taskType === 'unknown_part_import'}
          <ListingSelector
            {enrichment}
            {preferredDistId}
            bind:primaryListingKey
            bind:includedListings
            onRetryDistributor={handleRetryDistributor}
          />
          <div class="grid grid-cols-2 gap-4 pt-4 mt-4 border-t border-border">
            <div>
              <label for="import-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
              <input id="import-qty" type="number" min="0" bind:value={importInitialQty} placeholder="0"
                class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
            </div>
            {#if parseInt(importInitialQty, 10) > 0}
              <div>
                <label for="import-reason" class="text-xs font-medium text-muted-foreground block mb-1">Reason</label>
                <select id="import-reason" bind:value={importInitialReason}
                  class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring">
                  <option value="initial_count">Initial count</option>
                  <option value="received">Received</option>
                  <option value="correction">Correction</option>
                </select>
              </div>
            {/if}
          </div>
          <div class="pt-3">
            <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
            <StorageLocationPicker value={importStorageLoc} {locations} onSelect={(v) => { importStorageLoc = v; }} />
          </div>

        {:else if step === 'review' && taskType === 'known_part_create_entry'}
          <div class="rounded-md border border-info/30 bg-info-subtle p-3 text-sm mb-4">
            <p class="font-medium text-info">Already in your catalog</p>
            <p class="text-info mt-0.5">This part exists but has no inventory entry for your team. Create one below.</p>
          </div>
          {#if activePart}
            <div class="rounded-lg border border-border bg-card p-4 mb-4 space-y-1.5 text-sm">
              {#if activePart.mpn}<div class="flex justify-between"><span class="text-muted-foreground">MPN</span><span class="font-mono font-medium">{activePart.mpn}</span></div>{/if}
              {#if activePart.manufacturer}<div class="flex justify-between"><span class="text-muted-foreground">Manufacturer</span><span>{activePart.manufacturer}</span></div>{/if}
              {#if activePart.description}<p class="text-xs text-muted-foreground">{activePart.description}</p>{/if}
            </div>
          {/if}
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label for="create-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
              <input id="create-qty" type="number" min="0" bind:value={createQty} placeholder="0"
                class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
            </div>
            <div>
              <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
              <StorageLocationPicker value={createStorageLoc} {locations} onSelect={(v) => { createStorageLoc = v; }} />
            </div>
          </div>

        {:else if step === 'review' && taskType === 'known_part_stock_adjustment'}
          <div class="rounded-md border border-info/30 bg-info-subtle p-3 text-sm mb-4">
            <p class="font-medium text-info">Already in your inventory</p>
            <p class="text-info mt-0.5">Adjust the stock level for this existing part.</p>
          </div>
          {#if activePart}
            <div class="rounded-lg border border-border bg-card p-4 mb-4 text-sm">
              <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2">
                {#if activePart.mpn}<dt class="text-muted-foreground">MPN</dt><dd class="font-mono font-medium">{activePart.mpn}</dd>{/if}
                {#if activePart.manufacturer}<dt class="text-muted-foreground">Manufacturer</dt><dd>{activePart.manufacturer}</dd>{/if}
                <dt class="text-muted-foreground">Current stock</dt><dd class="font-medium tabular-nums">{activePart.quantity_on_hand ?? 0}</dd>
              </dl>
            </div>
            <AdjustStockForm
              currentQuantity={activePart.quantity_on_hand ?? undefined}
              bind:mode={adjustMode}
              bind:value={adjustQty}
              bind:reason={adjustReason}
            />
          {/if}

        {:else if step === 'manual'}
          {#if resolve?.status === 'failed' || resolve?.task_type === 'unrecognized'}
            <div class="rounded-md border border-warning/30 bg-warning-subtle p-3 text-sm mb-4">
              <p class="text-warning">No distributor match was found. You can still create the part manually.</p>
            </div>
          {/if}
          <div class="space-y-3">
            <div class="space-y-1">
              <Label>Scope</Label>
              <Select bind:value={manScope}>
                <option value="team">Team-scoped</option>
                <option value="global">Global</option>
              </Select>
            </div>
            <div class="space-y-1">
              <Label for="man-mpn">MPN</Label>
              <Input id="man-mpn" bind:value={manMpn} placeholder="e.g. MY-CUSTOM-01" oninput={() => (duplicateFound = null)} />
            </div>
            <div class="space-y-1">
              <Label for="man-mfr">Manufacturer</Label>
              <Input id="man-mfr" bind:value={manManufacturer} placeholder="e.g. Acme Internal" oninput={() => (duplicateFound = null)} />
            </div>
            <div class="space-y-1">
              <Label for="man-desc">Description</Label>
              <Input id="man-desc" bind:value={manDescription} placeholder="Short description" />
            </div>
            {#if manScope === 'team'}
              <div class="grid grid-cols-2 gap-4 pt-1">
                <div>
                  <label for="man-qty" class="text-xs font-medium text-muted-foreground block mb-1">Initial quantity</label>
                  <input id="man-qty" type="number" min="0" bind:value={manQty} placeholder="0"
                    class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-ring" />
                </div>
                <div>
                  <p class="text-xs font-medium text-muted-foreground mb-1">Storage location</p>
                  <StorageLocationPicker value={manStorageLoc} {locations} onSelect={(v) => { manStorageLoc = v; }} />
                </div>
              </div>
            {/if}
            {#if duplicateFound}
              <div class="rounded-md border border-warning/30 bg-warning-subtle p-3 text-sm">
                <p class="font-medium text-warning">This part already exists</p>
                <p class="mt-0.5 text-warning">
                  {duplicateFound.mpn ?? ''}
                  {#if duplicateFound.manufacturer} · {duplicateFound.manufacturer}{/if}
                  {#if duplicateFound.description} — {duplicateFound.description}{/if}
                </p>
              </div>
            {/if}
          </div>

        {:else if step === 'done'}
          <div class="py-8 text-center">
            <p class="text-sm font-medium">{doneMessage}</p>
            <p class="mt-1 text-xs text-muted-foreground">Add another part or close.</p>
          </div>
        {/if}

        {#if submitError}
          <p class="mt-4 text-sm text-destructive">{submitError}</p>
        {/if}
      </div>

      <!-- Action bar -->
      <div class="flex items-center justify-between gap-3 border-t border-border px-6 py-4">
        {#if step === 'entry'}
          <button type="button" class="text-sm text-muted-foreground hover:text-foreground" onclick={goManual}>Create manually instead</button>
          <div class="flex gap-2">
            <Button variant="outline" type="button" onclick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="button" disabled={!canResolve || submitting} onclick={doResolve}>{submitting ? 'Resolving…' : 'Resolve'}</Button>
          </div>

        {:else if step === 'resolving'}
          <span></span>
          <Button variant="outline" type="button" onclick={() => onOpenChange(false)}>Cancel</Button>

        {:else if step === 'review'}
          <Button variant="outline" type="button" onclick={resetForAnother}>Back</Button>
          <Button
            type="button"
            disabled={submitting || (taskType === 'unknown_part_import' && !primaryListingKey)}
            onclick={doCommit}
          >{submitting ? 'Committing…' : 'Commit part'}</Button>

        {:else if step === 'manual'}
          <Button variant="outline" type="button" onclick={resetForAnother}>Back</Button>
          <Button type="button" disabled={submitting || !!duplicateFound} onclick={doManualCreate}>{submitting ? 'Creating…' : 'Create part'}</Button>

        {:else if step === 'done'}
          <Button variant="outline" type="button" onclick={resetForAnother}>Commit &amp; add another</Button>
          <Button type="button" onclick={() => onOpenChange(false)}>Close</Button>
        {/if}
      </div>
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>

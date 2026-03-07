<script lang="ts">
  import type { Listing } from '$lib/types/parts.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { refreshAllListings, enrichPart, deleteListing } from '$lib/api/parts.js';
  import { updateInventoryEntry } from '$lib/api/inventory.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw, Star, Sparkles, Trash2, Plus, ChevronDown, ChevronRight } from 'lucide-svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import AddListingDialog from '$lib/components/parts/AddListingDialog.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import PriceBreaksTable from '$lib/components/common/PriceBreaksTable.svelte';

  let {
    partId,
    teamId,
    listings,
    entryId,
    preferredListingId,
    preferredDistributorId,
    onrefresh,
    onpreferredchange,
    ondelete,
    onadd,
  }: {
    partId: string;
    teamId?: string;
    listings: Listing[];
    entryId?: string | null;
    preferredListingId?: string | null;
    preferredDistributorId?: string | null;
    onrefresh?: () => void;
    onpreferredchange?: () => void;
    ondelete?: () => void;
    onadd?: () => void;
  } = $props();

  let sortedListings = $derived(
    preferredDistributorId
      ? [...listings].sort((a, b) => {
          const aPreferred = a.distributor_id === preferredDistributorId ? 0 : 1;
          const bPreferred = b.distributor_id === preferredDistributorId ? 0 : 1;
          return aPreferred - bPreferred;
        })
      : listings,
  );

  let expandedListingId = $state<string | null>(null);
  let refreshingAll = $state(false);
  let enrichingListing = $state<Set<string>>(new Set());
  let deleting = $state<Set<string>>(new Set());
  let settingPreferred = $state(false);
  let addListingOpen = $state(false);
  let confirmDeleteId = $state<string | null>(null);
  let confirmDeleteOpen = $state(false);

  async function doRefreshAll() {
    if (!teamId) return;
    refreshingAll = true;
    try {
      const { job_ids } = await refreshAllListings(partId, teamId);
      const total = job_ids.length;
      if (total === 0) return;
      let completed = 0;
      let failed = 0;
      for (const jobId of job_ids) {
        const unsub = subscribeSSE(teamId, jobId, (ev) => {
          if ((ev.data.status as string) === 'error') failed++;
          completed++;
          unsub();
          if (completed === total) {
            if (failed > 0) {
              pushToast('warning', 'Stock refresh incomplete', `${failed} of ${total} listing${total !== 1 ? 's' : ''} failed`);
            } else {
              pushToast('success', 'Stock refreshed', `${total} listing${total !== 1 ? 's' : ''} updated`);
            }
          }
        });
      }
    } catch (e: any) {
      pushToast('error', 'Refresh failed', e.message);
    } finally {
      refreshingAll = false;
    }
  }

  async function doEnrich(listing: Listing) {
    if (!teamId) return;
    enrichingListing = new Set([...enrichingListing, listing.id]);
    try {
      const { job_id } = await enrichPart(partId, teamId, { sku: listing.sku, distributor_id: listing.distributor_id });
      pushToast('info', 'Parameter refresh started', 'Parameters will update shortly.');
      const unsub = subscribeSSE(teamId, job_id, (ev) => {
        const status = ev.data.status as string;
        if (status === 'complete') {
          pushToast('success', 'Parameters updated', `${ev.data.parameters ?? 0} parameters, ${ev.data.listings ?? 0} listings`);
          unsub();
        } else if (status === 'not_found') {
          pushToast('warning', 'Part not found at distributor');
          unsub();
        } else if (status === 'error') {
          pushToast('error', 'Parameter refresh failed', String(ev.data.error ?? ''));
          unsub();
        }
      });
    } catch (e: any) {
      pushToast('error', 'Refresh failed', e.message);
    } finally {
      const s = new Set(enrichingListing);
      s.delete(listing.id);
      enrichingListing = s;
    }
  }

  async function doDelete(listingId: string) {
    if (!teamId) return;
    deleting = new Set([...deleting, listingId]);
    try {
      await deleteListing(partId, listingId, teamId);
      ondelete?.();
    } catch (e: any) {
      pushToast('error', 'Delete failed', e.message);
    } finally {
      const s = new Set(deleting);
      s.delete(listingId);
      deleting = s;
    }
    confirmDeleteId = null;
    confirmDeleteOpen = false;
  }

  async function setPreferred(listingId: string) {
    if (!teamId || !entryId) return;
    settingPreferred = true;
    const newId = listingId === preferredListingId ? null : listingId;
    try {
      await updateInventoryEntry(teamId, entryId, { preferred_listing_id: newId });
      onpreferredchange?.();
    } catch (e: any) {
      pushToast('error', 'Failed to update preferred listing', e.message);
    } finally {
      settingPreferred = false;
    }
  }
</script>

<div class="space-y-2">
  {#if teamId}
    <div class="flex justify-end gap-2">
      {#if listings.length > 0}
        <Button size="sm" variant="outline" disabled={refreshingAll} onclick={doRefreshAll}>
          <RefreshCw class="h-3.5 w-3.5 mr-1 {refreshingAll ? 'animate-spin' : ''}" /> Refresh Stock
        </Button>
      {/if}
      <Button size="sm" variant="outline" onclick={() => (addListingOpen = true)}>
        <Plus class="h-3.5 w-3.5 mr-1" /> Add Listing
      </Button>
    </div>
  {/if}

  <Table>
    <TableHeader>
      <TableRow>
        <TableHead class="w-6"></TableHead>
        <TableHead>Distributor</TableHead>
        <TableHead>SKU</TableHead>
        <TableHead>Packaging</TableHead>
        <TableHead>Unit Price</TableHead>
        <TableHead>Fixed Cost</TableHead>
        <TableHead>Stock</TableHead>
        <TableHead>MOQ</TableHead>
        <TableHead>Last Enriched</TableHead>
        <TableHead></TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {#each sortedListings as listing (listing.id)}
        {@const isPreferred = listing.id === preferredListingId}
        {@const hasBreaks = (listing.price_breaks?.length ?? 0) > 0}
        {@const isExpanded = expandedListingId === listing.id}
        <TableRow
          class="{isPreferred ? 'bg-accent/30' : ''} {hasBreaks ? 'cursor-pointer' : ''}"
          onclick={() => { if (hasBreaks) expandedListingId = isExpanded ? null : listing.id; }}
        >
          <TableCell class="text-muted-foreground pr-0">
            {#if hasBreaks}
              {#if isExpanded}
                <ChevronDown class="h-4 w-4" />
              {:else}
                <ChevronRight class="h-4 w-4" />
              {/if}
            {/if}
          </TableCell>
          <TableCell class="font-medium">
            {listing.distributor_name}
          </TableCell>
          <TableCell class="font-mono text-xs">{listing.sku}</TableCell>
          <TableCell>{listing.packaging_type ?? '—'}</TableCell>
          <TableCell>
            <PriceDisplay value={listing.unit_price} decimals={4} />
          </TableCell>
          <TableCell>
            {#if listing.fixed_order_cost && listing.fixed_order_cost !== '0' && listing.fixed_order_cost !== '0.00'}
              <PriceDisplay value={listing.fixed_order_cost} />
            {:else}
              <span class="text-muted-foreground">—</span>
            {/if}
          </TableCell>
          <TableCell>
            {#if listing.stock_available === null || listing.stock_available === undefined}
              <Badge variant="outline">Unknown</Badge>
            {:else if listing.stock_available === 0}
              <Badge variant="destructive">Out of stock</Badge>
            {:else}
              <Badge variant="success">{listing.stock_available}</Badge>
            {/if}
          </TableCell>
          <TableCell>{listing.moq ?? 1}</TableCell>
          <TableCell class="text-xs text-muted-foreground">
            <DateDisplay value={listing.last_refreshed_at} />
          </TableCell>
          <TableCell onclick={(e: MouseEvent) => e.stopPropagation()}>
            <div class="flex items-center gap-1">
              {#if entryId}
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={settingPreferred}
                  onclick={() => setPreferred(listing.id)}
                  title={isPreferred ? 'Clear preferred listing' : 'Set as preferred listing'}
                >
                  <Star class="h-3.5 w-3.5 {isPreferred ? 'fill-yellow-400 text-yellow-400' : 'text-muted-foreground'}" />
                </Button>
              {/if}
              {#if teamId}
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={enrichingListing.has(listing.id)}
                  onclick={() => doEnrich(listing)}
                  title="Refresh parameters"
                >
                  <Sparkles class="h-3.5 w-3.5 {enrichingListing.has(listing.id) ? 'animate-pulse' : ''}" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={deleting.has(listing.id)}
                  onclick={() => { confirmDeleteId = listing.id; confirmDeleteOpen = true; }}
                  title="Delete listing"
                >
                  <Trash2 class="h-3.5 w-3.5 text-destructive" />
                </Button>
              {/if}
            </div>
          </TableCell>
        </TableRow>
        {#if isExpanded && hasBreaks}
          <TableRow class="bg-muted/30 hover:bg-muted/30">
            <TableCell colspan={10} class="py-3 px-4">
              <PriceBreaksTable breaks={listing.price_breaks!} flatPrice={listing.unit_price} />
            </TableCell>
          </TableRow>
        {/if}
      {:else}
        <TableRow>
          <TableCell colspan={10} class="text-center text-muted-foreground py-8">No listings yet</TableCell>
        </TableRow>
      {/each}
    </TableBody>
  </Table>
</div>

{#if teamId}
  <AddListingDialog
    {partId}
    {teamId}
    bind:open={addListingOpen}
    onadded={() => { addListingOpen = false; onadd?.(); }}
  />

  <ConfirmDialog
    bind:open={confirmDeleteOpen}
    title="Delete listing?"
    description="This will remove the listing and all distributor-sourced parameters from this distributor. User-specified parameters are unaffected."
    confirmLabel="Delete"
    destructive
    onconfirm={() => confirmDeleteId && doDelete(confirmDeleteId)}
    oncancel={() => (confirmDeleteId = null)}
  />
{/if}

<script lang="ts">
  import type { Listing } from '$lib/types/parts.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { refreshListing, enrichPart, deleteListing } from '$lib/api/parts.js';
  import { updateInventoryEntry } from '$lib/api/inventory.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw, Star, Sparkles, Trash2, Plus } from 'lucide-svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import AddListingDialog from '$lib/components/parts/AddListingDialog.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';

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

  let refreshing = $state<Set<string>>(new Set());
  let enrichingListing = $state<Set<string>>(new Set());
  let deleting = $state<Set<string>>(new Set());
  let settingPreferred = $state(false);
  let addListingOpen = $state(false);
  let confirmDeleteId = $state<string | null>(null);
  let confirmDeleteOpen = $state(false);

  async function doRefresh(listingId: string) {
    refreshing = new Set([...refreshing, listingId]);
    try {
      await refreshListing(partId, listingId, teamId);
      onrefresh?.();
    } catch (e: any) {
      pushToast('error', 'Refresh failed', e.message);
    } finally {
      const s = new Set(refreshing);
      s.delete(listingId);
      refreshing = s;
    }
  }

  async function doEnrich(listing: Listing) {
    if (!teamId) return;
    enrichingListing = new Set([...enrichingListing, listing.id]);
    try {
      await enrichPart(partId, teamId, { sku: listing.sku, distributor_id: listing.distributor_id });
      pushToast('info', 'Parameter refresh started', 'Parameters will update shortly.');
      onrefresh?.();
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
    <div class="flex justify-end">
      <Button size="sm" variant="outline" onclick={() => (addListingOpen = true)}>
        <Plus class="h-3.5 w-3.5 mr-1" /> Add Listing
      </Button>
    </div>
  {/if}

  <Table>
    <TableHeader>
      <TableRow>
        <TableHead>Distributor</TableHead>
        <TableHead>SKU</TableHead>
        <TableHead>Packaging</TableHead>
        <TableHead>Unit Price</TableHead>
        <TableHead>Stock</TableHead>
        <TableHead>MOQ</TableHead>
        <TableHead>Last Enriched</TableHead>
        <TableHead></TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {#each sortedListings as listing (listing.id)}
        {@const isPreferred = listing.id === preferredListingId}
        <TableRow class={isPreferred ? 'bg-accent/30' : ''}>
          <TableCell class="font-medium">
            {listing.distributor_name}
          </TableCell>
          <TableCell class="font-mono text-xs">{listing.sku}</TableCell>
          <TableCell>{listing.packaging_type ?? '—'}</TableCell>
          <TableCell>
            {#if listing.unit_price != null}
              ${parseFloat(listing.unit_price).toFixed(4)}
            {:else}
              —
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
          <TableCell>
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
              <Button
                variant="ghost"
                size="icon"
                disabled={refreshing.has(listing.id)}
                onclick={() => doRefresh(listing.id)}
                title="Refresh stock data"
              >
                <RefreshCw class="h-3.5 w-3.5 {refreshing.has(listing.id) ? 'animate-spin' : ''}" />
              </Button>
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
      {:else}
        <TableRow>
          <TableCell colspan={8} class="text-center text-muted-foreground py-8">No listings yet</TableCell>
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

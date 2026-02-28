<script lang="ts">
  import type { Listing } from '$lib/types/parts.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { refreshListing } from '$lib/api/parts.js';
  import { updateInventoryEntry } from '$lib/api/inventory.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw, Star } from 'lucide-svelte';

  let {
    partId,
    teamId,
    listings,
    entryId,
    preferredListingId,
    onrefresh,
    onpreferredchange,
  }: {
    partId: string;
    teamId?: string;
    listings: Listing[];
    entryId?: string | null;
    preferredListingId?: string | null;
    onrefresh?: () => void;
    onpreferredchange?: () => void;
  } = $props();

  let refreshing = $state<Set<string>>(new Set());
  let settingPreferred = $state(false);

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

  async function setPreferred(listingId: string) {
    if (!teamId || !entryId) return;
    settingPreferred = true;
    // Toggle: clicking the already-preferred listing clears it.
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

<Table>
  <TableHeader>
    <TableRow>
      <TableHead>Distributor</TableHead>
      <TableHead>SKU</TableHead>
      <TableHead>Packaging</TableHead>
      <TableHead>Unit Price</TableHead>
      <TableHead>Stock</TableHead>
      <TableHead>MOQ</TableHead>
      <TableHead></TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each listings as listing (listing.id)}
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
          </div>
        </TableCell>
      </TableRow>
    {:else}
      <TableRow>
        <TableCell colspan={7} class="text-center text-muted-foreground py-8">No listings yet</TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>

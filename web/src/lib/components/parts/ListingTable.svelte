<script lang="ts">
  import type { Listing } from '$lib/types/parts.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { refreshListing } from '$lib/api/parts.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { RefreshCw } from 'lucide-svelte';

  let {
    partId,
    teamId,
    listings,
    onrefresh,
  }: {
    partId: string;
    teamId?: string;
    listings: Listing[];
    onrefresh?: () => void;
  } = $props();

  let refreshing = $state<Set<string>>(new Set());

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
      <TableRow>
        <TableCell class="font-medium">{listing.distributor_name}</TableCell>
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
          <Button
            variant="ghost"
            size="icon"
            disabled={refreshing.has(listing.id)}
            onclick={() => doRefresh(listing.id)}
            title="Refresh stock data"
          >
            <RefreshCw class="h-3.5 w-3.5 {refreshing.has(listing.id) ? 'animate-spin' : ''}" />
          </Button>
        </TableCell>
      </TableRow>
    {:else}
      <TableRow>
        <TableCell colspan={7} class="text-center text-muted-foreground py-8">No listings yet</TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>

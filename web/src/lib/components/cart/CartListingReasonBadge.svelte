<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import type { CartListing } from '$lib/types/orders.js';

  let { listing }: { listing: CartListing | null } = $props();

  // Only surface a chip when the sourcing policy did something noteworthy — the expected
  // outcomes ("preferred", "cost", "only_option") stay quiet to avoid row noise.
  type ReasonInfo = { label: string; variant: 'warning' | 'secondary'; title: string };
  const REASONS: Record<string, ReasonInfo> = {
    preferred_fallback: {
      label: 'Fallback',
      variant: 'warning',
      title: 'Your top-ranked distributor was out of stock — used the next in your ranking.',
    },
    out_of_stock_fallback: {
      label: 'Cheapest',
      variant: 'secondary',
      title: 'No ranked distributor had stock — fell back to the lowest-cost listing.',
    },
    over_tolerance: {
      label: 'Over budget',
      variant: 'warning',
      title: 'Your preferred distributor exceeded the cost tolerance — used the cheaper listing.',
    },
  };

  let info = $derived(listing?.reason ? REASONS[listing.reason] : undefined);
</script>

{#if info}
  <span title={info.title} class="inline-flex">
    <Badge variant={info.variant} class="text-[10px] px-1 py-0">{info.label}</Badge>
  </span>
{/if}
{#if listing?.preferred_stale}
  <span title="Selected on stock data older than the distributor's cache window — refresh to confirm." class="inline-flex">
    <Badge variant="warning" class="text-[10px] px-1 py-0">Stale stock</Badge>
  </span>
{/if}

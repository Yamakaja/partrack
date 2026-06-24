<script lang="ts">
  import { ExternalLink, Star } from 'lucide-svelte';
  import type { EnrichmentDataAggregate } from '$lib/api/bulkScan.js';

  let {
    enrichment,
    preferredDistId = null,
    primaryListingKey = $bindable(''),
    includedListings = $bindable(new Set<string>()),
    onRetryDistributor,
  }: {
    enrichment: EnrichmentDataAggregate | null;
    preferredDistId?: string | null;
    primaryListingKey?: string;
    includedListings?: Set<string>;
    onRetryDistributor?: (distributorId: string) => void;
  } = $props();

  const dists = $derived(enrichment?.distributors ?? []);

  // Key format: first 36 chars = distributor UUID, char 37 = ':', rest = SKU.
  // UUIDs never contain ':' so this is unambiguous regardless of SKU content.
  function listingKey(distId: string, sku: string): string {
    return `${distId}:${sku}`;
  }

  function findInitialPrimaryListingKey(): string {
    const withListings = dists.filter((d) =>
      (d.results ?? []).some((r) => (r.listings?.length ?? 0) > 0),
    );
    if (withListings.length === 0) return '';
    const preferred =
      withListings.find((d) => d.distributor_id === preferredDistId) ?? withListings[0];
    const firstResult = preferred.results?.find((r) => (r.listings?.length ?? 0) > 0);
    if (!firstResult) return '';
    return listingKey(preferred.distributor_id, firstResult.listings![0].sku);
  }

  // Initialise listing selection once enrichment data is available. The parent
  // resets primaryListingKey to '' whenever the underlying enrichment changes.
  $effect(() => {
    if (!enrichment || primaryListingKey !== '') return;
    const pk = findInitialPrimaryListingKey();
    primaryListingKey = pk;
    const included = new Set<string>();
    for (const d of enrichment.distributors) {
      for (const result of d.results ?? []) {
        for (const listing of result.listings ?? []) {
          const key = listingKey(d.distributor_id, listing.sku);
          if (key !== pk) included.add(key);
        }
      }
    }
    includedListings = included;
  });

  function setPrimaryListing(key: string) {
    const s = new Set(includedListings);
    if (primaryListingKey) s.add(primaryListingKey);
    s.delete(key);
    primaryListingKey = key;
    includedListings = s;
  }

  function toggleListing(key: string) {
    const s = new Set(includedListings);
    if (s.has(key)) s.delete(key); else s.add(key);
    includedListings = s;
  }

  function formatPrice(price: string | undefined): string {
    if (!price) return '';
    const n = parseFloat(price);
    return isNaN(n) ? price : `$${n.toFixed(4)}`;
  }

  // Part info header — use the product that contains the primary listing.
  const primaryDistId = $derived(primaryListingKey.slice(0, 36));
  const primarySku = $derived(primaryListingKey.slice(37));
  const primaryDist = $derived(dists.find((d) => d.distributor_id === primaryDistId));
  const partInfo = $derived(
    primaryDist?.results?.find((r) => r.listings?.some((l) => l.sku === primarySku)) ??
    primaryDist?.results?.[0] ??
    dists.find((d) => d.results?.length)?.results?.[0] ??
    null,
  );
</script>

<div class="space-y-6">
  {#if partInfo}
    <div class="rounded-lg border border-border bg-card p-4 space-y-1.5 text-sm">
      {#if partInfo.mpn}
        <div class="flex justify-between gap-4">
          <span class="text-muted-foreground">MPN</span>
          <span class="font-mono font-medium">{partInfo.mpn}</span>
        </div>
      {/if}
      {#if partInfo.manufacturer}
        <div class="flex justify-between gap-4">
          <span class="text-muted-foreground">Manufacturer</span>
          <span>{partInfo.manufacturer}</span>
        </div>
      {/if}
      {#if partInfo.description}
        <p class="text-xs text-muted-foreground pt-0.5">{partInfo.description}</p>
      {/if}
      {#if partInfo.datasheet_url}
        <a href={partInfo.datasheet_url} target="_blank" rel="noopener noreferrer"
          class="text-xs text-primary hover:underline">Datasheet ↗</a>
      {/if}
    </div>
  {/if}

  <!-- Distributor sections -->
  <div class="space-y-3">
    {#each dists as dist (dist.distributor_id)}
      {@const distHasPrimary =
        (dist.results ?? []).some((r) =>
          r.listings?.some((l) => listingKey(dist.distributor_id, l.sku) === primaryListingKey),
        )}
      {@const hasListings = (dist.results ?? []).some((r) => (r.listings?.length ?? 0) > 0)}
      <div class="rounded-lg border overflow-hidden {distHasPrimary ? 'border-primary/60' : 'border-border'}">
        <!-- Header -->
        <div class="flex items-center gap-3 px-4 py-2.5 bg-muted/40 border-b border-border">
          <span class="text-sm font-medium flex-1 truncate">{dist.name}</span>
          {#if dist.error && onRetryDistributor}
            <button
              type="button"
              onclick={() => onRetryDistributor?.(dist.distributor_id)}
              class="shrink-0 rounded border border-border px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent/50"
            >Retry</button>
          {/if}
        </div>

        {#if dist.error}
          <p class="px-4 py-2.5 text-xs text-destructive/80">{dist.error.split('\n')[0]}</p>
        {:else if hasListings}
          <div class="divide-y divide-border">
            {#each (dist.results ?? []) as result, ri (ri)}
              {#if (result.listings?.length ?? 0) > 0}
                <!-- Product header -->
                <div class="px-4 py-2 bg-muted/20 border-b border-border/60">
                  <div class="flex items-center gap-2 flex-wrap">
                    {#if result.mpn}
                      <span class="font-mono text-xs font-semibold">{result.mpn}</span>
                    {/if}
                    {#if result.manufacturer}
                      <span class="text-xs text-muted-foreground">{result.manufacturer}</span>
                    {/if}
                  </div>
                  {#if result.description}
                    <p class="text-xs text-muted-foreground mt-0.5 line-clamp-2">{result.description}</p>
                  {/if}
                </div>
                {#each result.listings! as listing}
                  {@const lKey = listingKey(dist.distributor_id, listing.sku)}
                  {@const isListingPrimary = lKey === primaryListingKey}
                  {@const isIncluded = isListingPrimary || includedListings.has(lKey)}
                  <div class="px-3 py-2 flex items-center gap-2.5 text-xs
                    {isListingPrimary ? 'bg-accent/20' : ''}
                    {!isIncluded ? 'opacity-40' : ''}">
                    <!-- Include checkbox (disabled for primary — always included) -->
                    <input
                      type="checkbox"
                      checked={isIncluded}
                      disabled={isListingPrimary}
                      onchange={() => toggleListing(lKey)}
                      class="h-3.5 w-3.5 rounded shrink-0"
                    />
                    <!-- SKU -->
                    <span class="font-mono flex-1 min-w-0 truncate">{listing.sku}</span>
                    <!-- Marketplace badge -->
                    {#if listing.marketplace}
                      <span
                        class="inline-flex items-center rounded-full border border-warning/40 bg-warning-subtle text-warning px-2 py-0.5 font-semibold shrink-0"
                        title="Third-party marketplace seller — not stocked by the distributor"
                      >Marketplace</span>
                    {/if}
                    <!-- Packaging -->
                    {#if listing.packaging_type}
                      <span class="text-muted-foreground shrink-0">{listing.packaging_type}</span>
                    {/if}
                    <!-- Price -->
                    {#if listing.unit_price}
                      <span class="tabular-nums font-medium shrink-0">{formatPrice(listing.unit_price)}/ea</span>
                    {/if}
                    <!-- Stock badge -->
                    {#if listing.stock_available === null || listing.stock_available === undefined}
                      <span class="inline-flex items-center rounded-full border border-border px-2 py-0.5 font-semibold shrink-0">?</span>
                    {:else if listing.stock_available === 0}
                      <span class="inline-flex items-center rounded-full border-transparent bg-destructive text-destructive-foreground px-2 py-0.5 font-semibold shrink-0">0</span>
                    {:else}
                      <span class="inline-flex items-center rounded-full border-transparent bg-success-subtle text-success px-2 py-0.5 font-semibold shrink-0">{listing.stock_available.toLocaleString()}</span>
                    {/if}
                    <!-- MOQ -->
                    <span class="text-muted-foreground shrink-0">MOQ {listing.moq}</span>
                    <!-- Inline action buttons (ListingTable style) -->
                    <div class="flex items-center gap-0.5 shrink-0 ml-auto">
                      {#if listing.listing_url}
                        <a
                          href={listing.listing_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          class="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                          title="Open product page"
                        >
                          <ExternalLink class="h-3.5 w-3.5" />
                        </a>
                      {/if}
                      <button
                        type="button"
                        onclick={() => setPrimaryListing(lKey)}
                        title={isListingPrimary ? 'Primary listing (used for enrichment)' : 'Set as primary listing'}
                        class="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                      >
                        <Star class="h-3.5 w-3.5 {isListingPrimary ? 'fill-yellow-400 text-yellow-400' : ''}" />
                      </button>
                    </div>
                  </div>
                {/each}
              {/if}
            {/each}
          </div>
        {:else if !dist.error}
          <p class="px-4 py-2.5 text-xs text-muted-foreground">No listings found.</p>
        {/if}
      </div>
    {/each}
  </div>
</div>

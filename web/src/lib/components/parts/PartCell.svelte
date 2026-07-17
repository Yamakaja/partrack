<script lang="ts">
  import { Cpu } from 'lucide-svelte';
  import PartRef from './PartRef.svelte';
  import type { PartFlag } from '$lib/api/notes.js';
  import type { LifecycleStatus } from '$lib/types/parts.js';

  // Two-line table-cell variant of PartRef: a thumbnail tile beside an identity
  // line (ref + optional manufacturer) with a truncated muted description
  // underneath. The identity fields are MANDATORY so every table renders the
  // same part representation — callers must thread the full set (a null value
  // is fine; an omitted prop is not). Behavioural modifiers (link/hover/flag)
  // stay optional.
  let {
    partId,
    mpn,
    teamId,
    manufacturer,
    description,
    lifecycle,
    imageUrl,
    availableQty,
    neededQty = null,
    stockRingClass = 'ring-background',
    flag,
    link = true,
    hover = true,
  }: {
    partId: string;
    mpn: string | null;
    teamId: string;
    manufacturer: string | null;
    description: string | null;
    lifecycle: LifecycleStatus | string | null;
    /** Thumbnail source; null renders the placeholder tile (keeps rows aligned). */
    imageUrl: string | null;
    /**
     * Available (free) stock. Enables the corner stock dot when supplied
     * (omit for surfaces where stock is meaningless). `null` = not tracked
     * (grey dot); a number is coloured against `neededQty`.
     */
    availableQty?: number | null;
    /** Threshold to compare available stock against; enables the "low" state. */
    neededQty?: number | null;
    /** Ring colour behind the stock dot — match the surrounding surface bg. */
    stockRingClass?: string;
    flag?: PartFlag;
    link?: boolean;
    hover?: boolean;
  } = $props();

  const label = $derived(mpn ?? description);
  // Hide the second line when it would just repeat the identity line
  // (generic parts render their description as the label).
  const secondary = $derived(description && description !== label ? description : null);

  // Stock dot: red = out, orange = below the needed qty, green = in stock /
  // enough available, grey = not tracked. Orange only ever appears when a
  // needed qty is present — the colour itself distinguishes the two modes.
  type Stock = { class: string; title: string };
  const stock = $derived.by<Stock | null>(() => {
    if (availableQty === undefined) return null; // surface opted out
    if (availableQty === null) return { class: 'bg-muted-foreground/40', title: 'Not tracked in inventory' };
    const avail = Math.max(0, availableQty);
    if (availableQty <= 0) return { class: 'bg-danger', title: 'Out of stock' };
    if (neededQty != null && neededQty > 0 && availableQty < neededQty)
      return { class: 'bg-warning', title: `Only ${avail.toLocaleString()} of ${neededQty.toLocaleString()} available` };
    if (neededQty != null && neededQty > 0)
      return { class: 'bg-success', title: `${avail.toLocaleString()} available (need ${neededQty.toLocaleString()})` };
    return { class: 'bg-success', title: `${avail.toLocaleString()} in stock` };
  });
</script>

<div class="flex min-w-0 items-center gap-2.5">
  <!-- Thumbnail: distributor photos stay on a white tile even in dark mode
       (mirrors the hover card); no image falls back to a muted chip icon.
       A stock dot rides the bottom-right corner when stock data is supplied. -->
  <div class="relative shrink-0">
    <div
      class="flex h-8 w-8 items-center justify-center overflow-hidden rounded border border-border {imageUrl
        ? 'bg-white'
        : 'bg-muted'}"
    >
      {#if imageUrl}
        <img src={imageUrl} alt="" class="h-full w-full object-contain p-0.5" />
      {:else}
        <Cpu class="h-4 w-4 text-muted-foreground/40" />
      {/if}
    </div>
    {#if stock}
      <span
        class="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 {stockRingClass} {stock.class}"
        title={stock.title}
      ></span>
    {/if}
  </div>

  <div class="min-w-0">
    <div class="flex min-w-0 items-baseline gap-2">
      <PartRef {partId} {mpn} {teamId} {lifecycle} {flag} fallback={description} {link} {hover} />
      {#if manufacturer}
        <span class="truncate text-[11px] text-muted-foreground">{manufacturer}</span>
      {/if}
    </div>
    {#if secondary}
      <p class="mt-0.5 max-w-[31rem] truncate text-[11px] text-muted-foreground" title={secondary}>
        {secondary}
      </p>
    {/if}
  </div>
</div>

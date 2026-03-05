<script lang="ts">
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';

  interface PriceBreak {
    break_qty: number;
    unit_price: string;
  }

  let { breaks, qty, prevQty, flatPrice }: {
    breaks: PriceBreak[];
    /** If provided, highlights the active price tier and shows a "@ N" subtotal column. */
    qty?: number;
    /** If provided and different from qty, shows a strikethrough subtotal on the tier that
     *  would have applied at this quantity (e.g. before a price-break bump). */
    prevQty?: number;
    /** Shown when breaks is empty. */
    flatPrice?: string | null;
  } = $props();

  function findTier(q: number): number {
    let active = -1;
    for (const pb of breaks) {
      if (q >= pb.break_qty && pb.break_qty > active) active = pb.break_qty;
    }
    return active;
  }

  let activeTier = $derived(qty !== undefined ? findTier(qty) : -1);
  let prevTier = $derived(
    prevQty !== undefined && prevQty !== qty ? findTier(prevQty) : -1
  );
</script>

{#if breaks.length === 0}
  <p class="text-xs text-muted-foreground">
    {#if flatPrice}
      Flat rate: <PriceDisplay value={flatPrice} decimals={4} />
    {:else}
      No pricing data
    {/if}
  </p>
{:else}
  <table class="text-xs">
    <thead>
      <tr class="text-muted-foreground">
        <th class="text-left font-normal pb-1 pr-4">Min Qty</th>
        <th class="text-right font-normal pb-1 {qty !== undefined ? 'pr-4' : ''}">Unit Price</th>
        {#if qty !== undefined}
          <th class="text-right font-normal pb-1">@ {qty}</th>
        {/if}
      </tr>
    </thead>
    <tbody>
      {#each breaks as pb}
        {@const isActive = qty !== undefined && pb.break_qty === activeTier}
        {@const isPrev = prevQty !== undefined && pb.break_qty === prevTier && prevTier !== activeTier}
        <tr class={isActive ? 'font-semibold text-foreground' : 'text-muted-foreground'}>
          <td class="py-0.5 pr-4">
            {#if isActive}<span class="mr-1">▶</span>{/if}{pb.break_qty.toLocaleString()}
          </td>
          <td class="text-right py-0.5 tabular-nums {qty !== undefined ? 'pr-4' : ''}">
            <PriceDisplay value={pb.unit_price} decimals={4} />
          </td>
          {#if qty !== undefined}
            <td class="text-right py-0.5 tabular-nums">
              {#if isActive}
                <PriceDisplay value={(parseFloat(pb.unit_price) * qty).toFixed(2)} />
              {:else if isPrev}
                <span class="line-through text-muted-foreground">
                  <PriceDisplay value={(parseFloat(pb.unit_price) * prevQty!).toFixed(2)} />
                </span>
              {/if}
            </td>
          {/if}
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

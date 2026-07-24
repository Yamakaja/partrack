<script lang="ts">
  import type { ObjectKind } from '$lib/types/objectRef.js';
  import PartHoverCard from '$lib/components/parts/PartHoverCard.svelte';
  import OrderHoverCard from './OrderHoverCard.svelte';
  import BuildHoverCard from './BuildHoverCard.svelte';

  // Picks the card for a kind. Kinds without one render nothing — the reference
  // still links, it just has no preview, so adding a card later is additive.
  //
  // Every card takes the same {id, teamId, open} contract and fetches on the
  // detail page's query key, so hovering warms navigation, navigation warms the
  // card, and SSE invalidation reaches both.
  let {
    kind,
    id,
    teamId,
    open,
  }: {
    kind: ObjectKind;
    id: string;
    teamId?: string;
    open: boolean;
  } = $props();
</script>

{#if kind === 'part'}
  <PartHoverCard {id} {teamId} {open} />
{:else if kind === 'order' && teamId}
  <OrderHoverCard {id} {teamId} {open} />
{:else if kind === 'build' && teamId}
  <BuildHoverCard {id} {teamId} {open} />
{/if}

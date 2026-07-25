<script lang="ts">
  import type { ObjectKind } from '$lib/types/objectRef.js';
  import PartHoverCard from '$lib/components/parts/PartHoverCard.svelte';
  import OrderHoverCard from './OrderHoverCard.svelte';
  import BuildHoverCard from './BuildHoverCard.svelte';
  import ProjectHoverCard from './ProjectHoverCard.svelte';
  import RevisionHoverCard from './RevisionHoverCard.svelte';

  // Picks the card for a kind. Kinds without one render nothing — the reference
  // still links, it just has no preview, so adding a card later is additive.
  //
  // Every card takes the same {id, teamId, open} contract and fetches on the
  // detail page's query key, so hovering warms navigation, navigation warms the
  // card, and SSE invalidation reaches both. A revision additionally needs its
  // project, which is the only nested kind.
  let {
    kind,
    id,
    teamId,
    parentId = null,
    open,
  }: {
    kind: ObjectKind;
    id: string;
    teamId?: string;
    parentId?: string | null;
    open: boolean;
  } = $props();
</script>

{#if kind === 'part'}
  <PartHoverCard {id} {teamId} {open} />
{:else if kind === 'order' && teamId}
  <OrderHoverCard {id} {teamId} {open} />
{:else if kind === 'build' && teamId}
  <BuildHoverCard {id} {teamId} {open} />
{:else if kind === 'project' && teamId}
  <ProjectHoverCard {id} {teamId} {open} />
{:else if kind === 'revision' && teamId && parentId}
  <RevisionHoverCard {id} {parentId} {teamId} {open} />
{/if}

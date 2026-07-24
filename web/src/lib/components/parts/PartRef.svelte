<script lang="ts">
  import type { PartFlag } from '$lib/api/notes.js';
  import type { LifecycleStatus } from '$lib/types/parts.js';
  import FlagBadge from '$lib/components/notes/FlagBadge.svelte';
  import ObjectRef from '$lib/components/objectref/ObjectRef.svelte';

  // Inline reference to a partrack Part — the part-shaped preset of ObjectRef.
  // The shell (hover card, lazy mount, scroll-close, linking) is shared; this
  // component owns only what is specific to parts: the MPN-or-description label
  // rule and the team opinion flag.
  //
  // Minimum contract is {partId, mpn}; everything richer comes from the card.
  let {
    partId,
    mpn,
    teamId,
    lifecycle = null,
    flag,
    fallback = null,
    link = true,
    hover = true,
    class: className = '',
  }: {
    partId: string;
    mpn: string | null | undefined;
    /** Enables the part-page link and team stock in the hover card. */
    teamId?: string;
    /** Renders a colored dot after the label when not 'active'. */
    lifecycle?: LifecycleStatus | string | null;
    /** Team opinion flag; renders a FlagBadge after the ref. */
    flag?: PartFlag;
    /** Label when mpn is null (generic parts), e.g. a genspec or description. */
    fallback?: string | null;
    link?: boolean;
    hover?: boolean;
    class?: string;
  } = $props();

  // A part with no MPN is generic: it is named by description rather than by a
  // manufacturer code, so it reads as a name (sans, italic) rather than a code.
  const generic = $derived(mpn == null);
</script>

<ObjectRef
  kind="part"
  id={partId}
  {teamId}
  label={mpn ?? null}
  fallback={generic ? (fallback ?? 'generic part') : null}
  status={lifecycle}
  mono={!generic}
  {link}
  {hover}
  class={className}
>
  {#snippet trailing()}
    {#if flag}<FlagBadge {flag} />{/if}
  {/snippet}
</ObjectRef>

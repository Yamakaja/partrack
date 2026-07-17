<script lang="ts">
  import { LinkPreview } from 'bits-ui';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { cn } from '$lib/utils/cn.js';
  import { closeOnScrollOutside } from '$lib/utils/closeOnScroll.js';
  import type { PartFlag } from '$lib/api/notes.js';
  import type { LifecycleStatus } from '$lib/types/parts.js';
  import FlagBadge from '$lib/components/notes/FlagBadge.svelte';
  import PartHoverCard from './PartHoverCard.svelte';

  // Inline reference to a partrack Part: quiet mono link with a dotted underline
  // (the app-wide hover-card affordance), lifecycle dot when not active, and an
  // italic-sans fallback label for generic parts (mpn == null).
  // Minimum contract is {partId, mpn}; everything richer comes from the hover card.
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

  let open = $state(false);
  // Card component (and its query observers) is only mounted after the first
  // hover — tables can render hundreds of refs, and idle rows must stay free.
  let everOpened = $state(false);

  // The card is anchored to its trigger, so scrolling would drag it across
  // the page — close it instead (scrolls inside the card keep it open).
  $effect(() => {
    if (!open) return;
    return closeOnScrollOutside('[data-link-preview-content]', () => (open = false));
  });

  const generic = $derived(mpn == null);
  const label = $derived(mpn ?? fallback ?? 'generic part');
  const linked = $derived(link && !!teamId);

  const DOT: Record<string, { class: string; title: string }> = {
    nrnd: { class: 'bg-warning', title: 'NRND' },
    deprecated: { class: 'bg-muted-foreground', title: 'Deprecated' },
    obsolete: { class: 'bg-danger', title: 'Obsolete' },
    no_longer_available: { class: 'bg-danger', title: 'No longer available' },
  };
  const dot = $derived(lifecycle ? DOT[lifecycle] : undefined);

  const refClass = $derived(
    cn(
      'font-mono font-medium text-xs',
      (hover || linked) &&
        'underline decoration-dotted decoration-foreground/30 underline-offset-[3px]',
      linked && 'hover:decoration-solid hover:decoration-foreground',
      generic && 'font-sans font-normal italic',
      className,
    ),
  );
</script>

{#snippet refContent()}
  {label}{#if dot}<span
      class={cn('ml-1 inline-block h-1.5 w-1.5 rounded-full align-[1px]', dot.class)}
      title={dot.title}
    ></span>{/if}
{/snippet}

<LinkPreview.Root
  {open}
  onOpenChange={(v) => {
    open = v;
    if (v) everOpened = true;
  }}
  openDelay={300}
  closeDelay={150}
  disabled={!hover}
>
  <LinkPreview.Trigger>
    {#snippet child({ props })}
      {#if linked}
        <a
          {...props}
          use:linkAction
          href="/teams/{teamId}/parts/{partId}"
          onclick={(e) => e.stopPropagation()}
          class={refClass}
        >{@render refContent()}</a>
      {:else}
        <span {...props} class={refClass}>{@render refContent()}</span>
      {/if}
    {/snippet}
  </LinkPreview.Trigger>
  {#if hover && everOpened}
    <PartHoverCard {partId} {teamId} {open} />
  {/if}
</LinkPreview.Root>
{#if flag}<FlagBadge {flag} />{/if}

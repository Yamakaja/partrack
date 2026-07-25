<script lang="ts">
  import type { Snippet } from 'svelte';
  import { LinkPreview } from 'bits-ui';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { cn } from '$lib/utils/cn.js';
  import { closeOnScrollOutside } from '$lib/utils/closeOnScroll.js';
  import { ensure, lookup } from '$lib/objectref/resolver.svelte.js';
  import { KIND_META, statusMeta, objectHref, TONE_BG } from '$lib/objectref/registry.js';
  import type { ObjectKind } from '$lib/types/objectRef.js';
  import ObjectHoverCard from './ObjectHoverCard.svelte';

  // Inline reference to any linkable partrack object: a quiet link with a dotted
  // underline (the app-wide hover-card affordance), a status dot when the state
  // is worth noticing mid-sentence, and a hover card for kinds that have one.
  //
  // Identity can be supplied or resolved. A caller holding the display fields —
  // which nearly every list row does — passes `label`, and the ref never touches
  // the network. A caller holding only an id passes neither, and the shared
  // resolver batches it with every other unresolved ref on the page.
  let {
    kind,
    id,
    teamId,
    label = null,
    status = null,
    href = null,
    fallback = null,
    mono = null,
    variant = 'inline',
    link = true,
    hover = true,
    class: className = '',
    trailing,
  }: {
    kind: ObjectKind;
    id: string;
    /** Enables linking, resolution, and team-scoped card content. */
    teamId?: string;
    /** Identity, when the caller already has it. Omit to resolve by id. */
    label?: string | null;
    status?: string | null;
    /** Route override — required for revisions, which nest under their project. */
    href?: string | null;
    /** Shown when neither the caller nor the resolver produced a label. */
    fallback?: string | null;
    /**
     * Override the kind's default typeface. A part identified by MPN is a code
     * (mono); the same part identified by description is a name (sans).
     */
    mono?: boolean | null;
    /** `chip` is the filled, icon-led form used inside note prose. */
    variant?: 'inline' | 'chip';
    link?: boolean;
    hover?: boolean;
    class?: string;
    /** Rendered after the reference, outside the link (e.g. a flag badge). */
    trailing?: Snippet;
  } = $props();

  let open = $state(false);
  // The card and its query observers mount only after the first hover — a table
  // can render hundreds of refs, and idle rows must stay free.
  let everOpened = $state(false);

  // The card is anchored to its trigger, so scrolling would drag it across the
  // page — close it instead (scrolls inside the card keep it open).
  $effect(() => {
    if (!open) return;
    return closeOnScrollOutside('[data-link-preview-content]', () => (open = false));
  });

  // Only reach for the resolver when identity wasn't handed to us.
  const needsResolve = $derived(!label && !!teamId && !!id);
  $effect(() => {
    if (needsResolve) ensure(teamId!, kind, id);
  });
  const resolved = $derived(needsResolve && teamId ? lookup(teamId, id) : undefined);

  const text = $derived(label ?? resolved?.label ?? fallback ?? '');
  const effectiveStatus = $derived(status ?? resolved?.status ?? null);
  const url = $derived(href ?? resolved?.href ?? (teamId ? objectHref(kind, teamId, id) : null));
  const linked = $derived(link && !!url);

  // Nothing to show: name the type rather than rendering an empty link, so a
  // deleted or still-loading reference stays readable.
  const placeholder = $derived(!text);
  const display = $derived(text || KIND_META[kind].label.toLowerCase());

  // A revision's card needs its project. The id alone can't supply it, but the
  // route can — every revision href nests under /projects/{id}/.
  const parentId = $derived.by(() => {
    if (kind !== 'revision' || !url) return null;
    return url.match(/\/projects\/([^/]+)\/revisions\//)?.[1] ?? null;
  });

  const isMono = $derived(mono ?? KIND_META[kind].mono);
  // A kind normally identified by a code, shown by name instead, is a stand-in
  // for the real identity — set in italics so it doesn't read as an MPN.
  const standIn = $derived(placeholder || (!isMono && KIND_META[kind].mono));

  const Icon = $derived(KIND_META[kind].icon);

  const dot = $derived.by(() => {
    const meta = statusMeta(kind, effectiveStatus);
    return meta?.dot ? { class: TONE_BG[meta.tone], title: meta.label } : null;
  });

  const refClass = $derived(
    variant === 'chip'
      ? cn(
          'inline-flex items-center gap-1 rounded border border-primary/30 bg-primary/10 px-1.5 py-0.5',
          'align-baseline text-sm font-medium text-primary no-underline hover:bg-primary/20',
          className,
        )
      : cn(
          'text-xs font-medium',
          isMono ? 'font-mono' : 'font-sans',
          (hover || linked) &&
            'underline decoration-dotted decoration-foreground/30 underline-offset-[3px]',
          linked && 'hover:decoration-solid hover:decoration-foreground',
          standIn && 'font-sans font-normal italic',
          className,
        ),
  );
</script>

{#snippet refContent()}
  {#if variant === 'chip'}<Icon class="h-3 w-3 shrink-0" />{/if}{display}{#if dot}<span
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
          href={url}
          title={variant === 'chip' ? display : undefined}
          onclick={(e) => e.stopPropagation()}
          class={refClass}
        >{@render refContent()}</a>
      {:else}
        <span {...props} class={refClass}>{@render refContent()}</span>
      {/if}
    {/snippet}
  </LinkPreview.Trigger>
  {#if hover && everOpened}
    <ObjectHoverCard {kind} {id} {teamId} {parentId} {open} />
  {/if}
</LinkPreview.Root>
{#if trailing}{@render trailing()}{/if}

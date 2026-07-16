<script lang="ts">
  import { LinkPreview } from 'bits-ui';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { kindIcon } from './objectKinds.js';
  import PartHoverCard from '$lib/components/parts/PartHoverCard.svelte';

  // Props arrive from the hast properties baked in by remarkManifest.
  let {
    kind = 'part',
    label = '',
    href = null,
    exists = 'false',
  }: {
    kind?: string;
    label?: string;
    href?: string | null;
    status?: string | null;
    exists?: string;
  } = $props();

  let Icon = $derived(kindIcon(kind));
  let isResolved = $derived(exists === 'true' && !!href);

  // Part chips get the shared hover card. The part/team ids aren't separate
  // props — recover them from the manifest href (server contract:
  // /teams/{teamId}/parts/{partId}, see handler/notes.go).
  const partRef = $derived.by(() => {
    if (kind !== 'part' || !href) return null;
    const m = href.match(/^\/teams\/([^/]+)\/parts\/([^/]+)$/);
    return m ? { teamId: m[1], partId: m[2] } : null;
  });

  let open = $state(false);
  let everOpened = $state(false);

  const chipClass =
    'inline-flex items-center gap-1 rounded border border-primary/30 bg-primary/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-primary no-underline hover:bg-primary/20';
</script>

{#snippet chip(extraProps: Record<string, unknown>)}
  <a {...extraProps} use:linkAction href={href!} title={label} class={chipClass}>
    <Icon class="h-3 w-3 shrink-0" />
    {label}
  </a>
{/snippet}

{#if isResolved && href}
  {#if partRef}
    <LinkPreview.Root
      {open}
      onOpenChange={(v) => {
        open = v;
        if (v) everOpened = true;
      }}
      openDelay={300}
      closeDelay={150}
    >
      <LinkPreview.Trigger>
        {#snippet child({ props })}
          {@render chip(props)}
        {/snippet}
      </LinkPreview.Trigger>
      {#if everOpened}
        <PartHoverCard partId={partRef.partId} teamId={partRef.teamId} {open} />
      {/if}
    </LinkPreview.Root>
  {:else}
    {@render chip({})}
  {/if}
{:else}
  <span
    title="Not found or no longer accessible"
    class="inline-flex items-center gap-1 rounded border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-destructive line-through"
  >
    <Icon class="h-3 w-3 shrink-0" />
    {label}
  </span>
{/if}

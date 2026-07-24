<script lang="ts">
  import { kindIcon } from '$lib/objectref/registry.js';
  import type { ObjectKind } from '$lib/types/objectRef.js';
  import ObjectRef from '$lib/components/objectref/ObjectRef.svelte';

  // Read-only note chip. Props arrive from the hast properties baked in by
  // remarkManifest, so label and href are already resolved server-side.
  //
  // The chip is ObjectRef's `chip` variant, which means every kind now gets the
  // hover card its type has — previously only parts did, because the card was
  // wired here by hand.
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

  // The manifest carries a route, not ids. Recover the team and object id from
  // it so the card can fetch (server contract: see handler/notes.go).
  const ids = $derived.by(() => {
    if (!href) return null;
    const m = href.match(/^\/teams\/([^/]+)\/(?:parts|projects|orders|builds|storage)\/([^/]+)$/);
    if (m) return { teamId: m[1], id: m[2] };
    // Revisions nest under their project: /teams/{t}/projects/{p}/revisions/{r}
    const rev = href.match(/^\/teams\/([^/]+)\/projects\/[^/]+\/revisions\/([^/]+)$/);
    return rev ? { teamId: rev[1], id: rev[2] } : null;
  });
</script>

{#if isResolved && href && ids}
  <ObjectRef
    kind={kind as ObjectKind}
    id={ids.id}
    teamId={ids.teamId}
    {label}
    {href}
    variant="chip"
  />
{:else if isResolved && href}
  <!-- Route we can't parse ids out of: still a link, just no preview. -->
  <ObjectRef kind={kind as ObjectKind} id="" {label} {href} variant="chip" hover={false} />
{:else}
  <span
    title="Not found or no longer accessible"
    class="inline-flex items-center gap-1 rounded border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-destructive line-through"
  >
    <Icon class="h-3 w-3 shrink-0" />
    {label}
  </span>
{/if}

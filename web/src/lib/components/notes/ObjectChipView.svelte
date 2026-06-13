<script lang="ts">
  import { linkAction } from '$lib/router/index.svelte.js';
  import { kindIcon } from './objectKinds.js';

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
</script>

{#if isResolved && href}
  <a
    use:linkAction
    {href}
    title={label}
    class="inline-flex items-center gap-1 rounded border border-primary/30 bg-primary/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-primary no-underline hover:bg-primary/20"
  >
    <Icon class="h-3 w-3 shrink-0" />
    {label}
  </a>
{:else}
  <span
    title="Not found or no longer accessible"
    class="inline-flex items-center gap-1 rounded border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-destructive line-through"
  >
    <Icon class="h-3 w-3 shrink-0" />
    {label}
  </span>
{/if}

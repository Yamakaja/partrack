<script lang="ts">
  import { Cpu } from 'lucide-svelte';
  import { linkAction } from '$lib/router/index.svelte.js';

  // Props arrive from the hast properties baked in by remarkManifest.
  let {
    label,
    mpn = null,
    href = null,
    resolved = 'false',
  }: {
    partId?: string;
    label?: string;
    mpn?: string | null;
    lifecycle?: string | null;
    href?: string | null;
    resolved?: string;
  } = $props();

  // Prefer the part's current MPN from the manifest; fall back to the label
  // stored in the body (its MPN at insert time), then a generic word.
  let text = $derived(mpn ?? label ?? 'part');
  let isResolved = $derived(resolved === 'true');
</script>

{#if href}
  <a
    use:linkAction
    {href}
    title={isResolved ? (mpn ?? label) : 'Part not found'}
    class="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 align-baseline text-sm font-medium no-underline
      {isResolved
      ? 'border-primary/30 bg-primary/10 text-primary hover:bg-primary/20'
      : 'border-destructive/30 bg-destructive/10 text-destructive line-through'}"
  >
    <Cpu class="h-3 w-3 shrink-0" />
    {text}
  </a>
{:else}
  <span class="inline-flex items-center gap-1 align-baseline text-sm font-medium text-muted-foreground">
    <Cpu class="h-3 w-3 shrink-0" />
    {text}
  </span>
{/if}

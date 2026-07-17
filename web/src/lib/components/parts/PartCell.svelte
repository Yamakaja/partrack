<script lang="ts">
  import { Cpu } from 'lucide-svelte';
  import PartRef from './PartRef.svelte';
  import type { PartFlag } from '$lib/api/notes.js';
  import type { LifecycleStatus } from '$lib/types/parts.js';

  // Two-line table-cell variant of PartRef: a thumbnail tile beside an identity
  // line (ref + optional manufacturer) with a truncated muted description
  // underneath. The identity fields are MANDATORY so every table renders the
  // same part representation — callers must thread the full set (a null value
  // is fine; an omitted prop is not). Behavioural modifiers (link/hover/flag)
  // stay optional.
  let {
    partId,
    mpn,
    teamId,
    manufacturer,
    description,
    lifecycle,
    imageUrl,
    flag,
    link = true,
    hover = true,
  }: {
    partId: string;
    mpn: string | null;
    teamId: string;
    manufacturer: string | null;
    description: string | null;
    lifecycle: LifecycleStatus | string | null;
    /** Thumbnail source; null renders the placeholder tile (keeps rows aligned). */
    imageUrl: string | null;
    flag?: PartFlag;
    link?: boolean;
    hover?: boolean;
  } = $props();

  const label = $derived(mpn ?? description);
  // Hide the second line when it would just repeat the identity line
  // (generic parts render their description as the label).
  const secondary = $derived(description && description !== label ? description : null);
</script>

<div class="flex min-w-0 items-center gap-2.5">
  <!-- Thumbnail: distributor photos stay on a white tile even in dark mode
       (mirrors the hover card); no image falls back to a muted chip icon. -->
  <div
    class="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded border border-border {imageUrl
      ? 'bg-white'
      : 'bg-muted'}"
  >
    {#if imageUrl}
      <img src={imageUrl} alt="" class="h-full w-full object-contain p-0.5" />
    {:else}
      <Cpu class="h-4 w-4 text-muted-foreground/40" />
    {/if}
  </div>

  <div class="min-w-0">
    <div class="flex min-w-0 items-baseline gap-2">
      <PartRef {partId} {mpn} {teamId} {lifecycle} {flag} fallback={description} {link} {hover} />
      {#if manufacturer}
        <span class="truncate text-[11px] text-muted-foreground">{manufacturer}</span>
      {/if}
    </div>
    {#if secondary}
      <p class="mt-0.5 max-w-[31rem] truncate text-[11px] text-muted-foreground" title={secondary}>
        {secondary}
      </p>
    {/if}
  </div>
</div>

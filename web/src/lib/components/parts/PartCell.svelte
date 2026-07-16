<script lang="ts">
  import PartRef from './PartRef.svelte';
  import type { PartFlag } from '$lib/api/notes.js';
  import type { LifecycleStatus } from '$lib/types/parts.js';

  // Two-line table-cell variant of PartRef: identity line (ref + optional
  // manufacturer) with a truncated muted description underneath. Both lines
  // come from the caller — the component stays dumb about API shapes.
  let {
    partId,
    mpn,
    teamId,
    manufacturer = null,
    description = null,
    lifecycle = null,
    flag,
    fallback = null,
    link = true,
    hover = true,
  }: {
    partId: string;
    mpn: string | null | undefined;
    teamId?: string;
    manufacturer?: string | null;
    description?: string | null;
    lifecycle?: LifecycleStatus | string | null;
    flag?: PartFlag;
    /** Label when mpn is null (generic parts). Defaults to the description. */
    fallback?: string | null;
    link?: boolean;
    hover?: boolean;
  } = $props();

  const label = $derived(mpn ?? fallback ?? description);
  // Hide the second line when it would just repeat the identity line
  // (generic parts render their description as the label).
  const secondary = $derived(description && description !== label ? description : null);
</script>

<div class="min-w-0">
  <div class="flex min-w-0 items-baseline gap-2">
    <PartRef {partId} {mpn} {teamId} {lifecycle} {flag} fallback={fallback ?? description} {link} {hover} />
    {#if manufacturer}
      <span class="truncate text-[11px] text-muted-foreground">{manufacturer}</span>
    {/if}
  </div>
  {#if secondary}
    <p class="mt-0.5 max-w-[24rem] truncate text-[11px] text-muted-foreground" title={secondary}>
      {secondary}
    </p>
  {/if}
</div>

<script lang="ts">
  import { avatarColor, avatarInitials } from '$lib/utils/avatarColor.js';
  import { cn } from '$lib/utils/cn.js';

  // Inline reference to a person. Users have no detail page and no avatar
  // column, so this is deliberately not an ObjectRef: there is nothing to link
  // to and nothing to fetch. It exists so a feed of actions reads as a feed of
  // *people* doing things rather than a column of bold names.
  //
  // The colour is derived from the id, so the same person is the same colour
  // everywhere — a weak identity cue, but a free one.
  let {
    name,
    seed = null,
    showName = true,
    class: className = '',
  }: {
    name: string | null | undefined;
    /** Stable colour seed — a user id when available, else the name. */
    seed?: string | null;
    showName?: boolean;
    class?: string;
  } = $props();

  const display = $derived((name ?? '').trim());
  const colors = $derived(avatarColor(seed || display || '?'));
  const initials = $derived(display ? avatarInitials(display) : '?');
</script>

{#if display}
  <span class={cn('inline-flex items-center gap-1.5 align-baseline', className)}>
    <span
      class="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[8px] font-semibold leading-none {colors.bg} {colors.text}"
      title={display}
      aria-hidden={showName ? 'true' : undefined}
    >{initials}</span>
    {#if showName}<span class="font-medium">{display}</span>{/if}
  </span>
{/if}

<script lang="ts">
  import type { Snippet } from 'svelte';
  import { LinkPreview } from 'bits-ui';

  // The portal, framing, and pending/error branches every object card shares, so
  // a card file contains only its own content bands. Fixed width keeps cards
  // comparable when a reader hovers several in a row.
  let {
    pending = false,
    error = false,
    errorText = "Couldn't load details.",
    children,
  }: {
    pending?: boolean;
    error?: boolean;
    errorText?: string;
    children: Snippet;
  } = $props();
</script>

<LinkPreview.Portal>
  <LinkPreview.Content
    side="bottom"
    align="start"
    sideOffset={8}
    class="z-50 w-[380px] rounded-lg border border-border bg-popover text-popover-foreground shadow-lg outline-none
           data-[state=open]:animate-in data-[state=closed]:animate-out
           data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
  >
    {#if pending}
      <div class="flex gap-3 p-3.5">
        <div class="h-14 w-14 shrink-0 animate-pulse rounded-md bg-muted"></div>
        <div class="flex-1 space-y-2 py-1">
          <div class="h-3.5 w-36 animate-pulse rounded bg-muted"></div>
          <div class="h-3 w-24 animate-pulse rounded bg-muted"></div>
          <div class="h-3 w-full animate-pulse rounded bg-muted"></div>
        </div>
      </div>
    {:else if error}
      <p class="p-3.5 text-xs text-muted-foreground">{errorText}</p>
    {:else}
      {@render children()}
    {/if}
  </LinkPreview.Content>
</LinkPreview.Portal>

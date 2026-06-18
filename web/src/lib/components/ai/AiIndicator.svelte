<script lang="ts">
  import { Popover } from 'bits-ui';
  import { Sparkles } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';
  import type { AiProvenance } from '$lib/api/revisions.js';

  let {
    ai = null,
    loading = false,
    failed = false,
  }: {
    /** Provenance to explain; required to show the popover. */
    ai?: AiProvenance | null;
    /** The AI is still reading this row — show a pulsing icon, no popover. */
    loading?: boolean;
    /** The AI read failed — show a muted icon with a tooltip, no popover. */
    failed?: boolean;
  } = $props();

  let open = $state(false);
</script>

{#if loading}
  <span
    class="inline-flex items-center gap-1 text-primary"
    title="AI is reading this row…"
  >
    <Sparkles class="h-3.5 w-3.5 animate-pulse" />
    <span class="text-[10px] uppercase tracking-wide">reading…</span>
  </span>
{:else if ai}
  <Popover.Root {open} onOpenChange={(v) => (open = v)}>
    <Popover.Trigger>
      <button
        type="button"
        class="inline-flex items-center gap-0.5 rounded px-0.5 text-primary transition-colors hover:bg-accent"
        title={ai.summary ?? 'Read by AI'}
        aria-label="How AI read this row"
      >
        <Sparkles class="h-3.5 w-3.5" />
        <span class="text-[10px] font-medium uppercase tracking-wide">AI</span>
      </button>
    </Popover.Trigger>

    <Popover.Content
      align="start"
      class="z-50 w-72 rounded-md border border-border bg-popover p-3 text-xs text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    >
      <div class="space-y-2">
        <div class="flex items-center gap-1.5 font-medium">
          <Sparkles class="h-3.5 w-3.5 text-primary" />
          Read by AI
        </div>

        {#if ai.summary}
          <p class="text-foreground">{ai.summary}</p>
        {/if}

        <div class="space-y-0.5 text-muted-foreground">
          {#if ai.model}<div>Model: <span class="font-mono">{ai.model}</span></div>{/if}
          {#if ai.reasoning_tokens}<div>Reasoning: {ai.reasoning_tokens.toLocaleString()} tokens</div>{/if}
        </div>

        {#if ai.reasoning}
          <details class="rounded border border-border">
            <summary class="cursor-pointer select-none px-2 py-1 text-muted-foreground hover:text-foreground">
              Show reasoning
            </summary>
            <pre class="max-h-60 overflow-auto whitespace-pre-wrap break-words border-t border-border px-2 py-1.5 text-[11px] leading-snug text-foreground">{ai.reasoning}</pre>
          </details>
        {:else}
          <p class="text-[11px] text-muted-foreground">
            This provider doesn't return the reasoning trace.
          </p>
        {/if}
      </div>
    </Popover.Content>
  </Popover.Root>
{:else if failed}
  <span
    class="inline-flex items-center gap-0.5 text-muted-foreground"
    title="AI could not read this row — using the automatic reading"
  >
    <Sparkles class="h-3.5 w-3.5 opacity-50" />
  </span>
{/if}

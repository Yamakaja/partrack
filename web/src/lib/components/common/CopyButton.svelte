<script lang="ts">
  import { Check, Copy } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';

  let {
    text,
    class: className = '',
  }: {
    text: string;
    class?: string;
  } = $props();

  let copied = $state(false);

  async function copy() {
    await navigator.clipboard.writeText(text);
    copied = true;
    setTimeout(() => (copied = false), 2000);
  }
</script>

<button
  type="button"
  onclick={copy}
  class={cn(
    'inline-flex items-center justify-center h-8 w-8 rounded-md border border-input bg-background text-muted-foreground hover:text-foreground transition-colors',
    className
  )}
  title="Copy to clipboard"
>
  {#if copied}
    <Check class="h-3.5 w-3.5 text-success" />
  {:else}
    <Copy class="h-3.5 w-3.5" />
  {/if}
</button>

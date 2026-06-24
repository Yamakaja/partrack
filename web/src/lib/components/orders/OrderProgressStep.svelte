<script lang="ts">
  import { Loader2, XCircle } from 'lucide-svelte';
  import { Button } from '$lib/components/ui/button/index.js';

  let {
    description,
    total,
    processed,
    processedLabel,
    unresolved,
    error,
    onClose,
  }: {
    description: string;
    total: number;
    processed: number;
    processedLabel?: string;
    unresolved?: string[];
    error: string;
    onClose: () => void;
  } = $props();

  let progressPct = $derived(total > 0 ? (processed / total) * 100 : 0);
  let unresolvedPct = $derived(total > 0 && unresolved?.length ? (unresolved.length / total) * 100 : 0);
</script>

{#if error}
  <div class="flex items-center gap-2 py-4 text-destructive">
    <XCircle class="h-5 w-5 shrink-0" />
    <p class="text-sm">{error}</p>
  </div>
  <div class="flex justify-end mt-4">
    <Button variant="outline" onclick={onClose}>Close</Button>
  </div>
{:else}
  <div class="flex items-center gap-3 py-4">
    <Loader2 class="h-5 w-5 shrink-0 animate-spin text-muted-foreground" />
    <div class="flex-1 space-y-1.5">
      <p class="text-sm text-muted-foreground">
        {description}{#if total > 0}&nbsp;{processed}/{total}{processedLabel ?? ''}{#if unresolved?.length}, {unresolved.length} unresolved{/if}{/if}
      </p>
      {#if total > 0}
        <div class="{unresolvedPct > 0 ? 'flex ' : ''}h-1.5 w-full overflow-hidden rounded-full bg-border">
          <div class="h-full bg-success transition-all" style="width: {progressPct}%"></div>
          {#if unresolvedPct > 0}
            <div class="h-full bg-destructive transition-all" style="width: {unresolvedPct}%"></div>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{/if}

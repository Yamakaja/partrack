<script lang="ts">
  import { subscribeSSE } from '$lib/api/sse.js';
  import { Loader2, CheckCircle, XCircle } from 'lucide-svelte';

  let {
    teamId,
    jobId,
    oncomplete,
  }: {
    teamId: string;
    jobId: string;
    oncomplete?: () => void;
  } = $props();

  let resolved = $state(0);
  let total = $state(0);
  let ambiguous = $state(0);
  let unresolved = $state(0);
  let done = $state(false);
  let error = $state('');

  $effect(() => {
    const unsub = subscribeSSE(teamId, jobId, (event) => {
      const d = event.data;
      if (d?.resolved !== undefined) resolved = d.resolved as number;
      if (d?.total !== undefined) total = d.total as number;
      if (d?.status === 'complete') {
        if (d?.ambiguous !== undefined) ambiguous = d.ambiguous as number;
        if (d?.unresolved !== undefined) unresolved = d.unresolved as number;
        done = true;
        oncomplete?.();
      } else if (d?.status === 'error') {
        error = (d.error as string) ?? 'Import failed';
      }
    });
    return unsub;
  });

  let pct = $derived(total > 0 ? Math.round((resolved / total) * 100) : 0);

  let completionDetail = $derived(() => {
    const parts: string[] = [];
    if (ambiguous > 0) parts.push(`${ambiguous} ambiguous`);
    if (unresolved > 0) parts.push(`${unresolved} unresolved`);
    return parts.length > 0 ? ` (${parts.join(', ')})` : '';
  });
</script>

<div class="flex items-center gap-3 rounded-md border border-border bg-muted/50 px-4 py-2 text-sm">
  {#if error}
    <XCircle class="h-4 w-4 shrink-0 text-destructive" />
    <span class="text-destructive">Import failed: {error}</span>
  {:else if done}
    <CheckCircle class="h-4 w-4 shrink-0 text-green-500" />
    <span>Import complete — {resolved} of {total} resolved{completionDetail()}</span>
  {:else}
    <Loader2 class="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
    <span class="text-muted-foreground">
      Importing BOM…
      {#if total > 0}
        {resolved}/{total} resolved
        <span class="ml-2 inline-block h-1.5 w-24 overflow-hidden rounded-full bg-border">
          <span
            class="block h-full rounded-full bg-primary transition-all"
            style="width: {pct}%"
          ></span>
        </span>
      {/if}
    </span>
  {/if}
</div>

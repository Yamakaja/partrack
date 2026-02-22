<script lang="ts">
  import { subscribeSSE } from '$lib/api/sse.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { CheckCircle, XCircle, Loader2 } from 'lucide-svelte';

  let {
    teamId,
    jobId,
    oncomplete,
  }: {
    teamId: string;
    jobId: string;
    oncomplete?: (data: Record<string, unknown>) => void;
  } = $props();

  type Status = 'pending' | 'running' | 'done' | 'error';
  let status = $state<Status>('pending');
  let errorMessage = $state('');

  $effect(() => {
    const unsub = subscribeSSE(teamId, jobId, (event) => {
      const s = event.data?.status as string | undefined;
      if (s === 'complete' || s === 'done') {
        status = 'done';
        oncomplete?.(event.data);
      } else if (s === 'error') {
        status = 'error';
        errorMessage = (event.data?.error as string) ?? 'Unknown error';
      } else {
        status = 'running';
      }
    });
    return unsub;
  });
</script>

<div class="flex items-center gap-2 text-sm">
  {#if status === 'pending'}
    <Skeleton class="h-4 w-4 rounded-full" />
    <span class="text-muted-foreground">Waiting…</span>
  {:else if status === 'running'}
    <Loader2 class="h-4 w-4 animate-spin text-muted-foreground" />
    <span class="text-muted-foreground">Processing…</span>
  {:else if status === 'done'}
    <CheckCircle class="h-4 w-4 text-green-500" />
    <Badge variant="success">Complete</Badge>
  {:else if status === 'error'}
    <XCircle class="h-4 w-4 text-destructive" />
    <Badge variant="destructive">Error: {errorMessage}</Badge>
  {/if}
</div>

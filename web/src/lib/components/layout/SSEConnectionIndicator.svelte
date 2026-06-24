<script lang="ts">
  import { Tooltip } from '$lib/components/ui/tooltip/index.js';
  import { sseConnectionStore } from '$lib/stores/sseConnection.svelte.js';

  const conn = $derived(sseConnectionStore.current);

  let now = $state(Date.now());

  $effect(() => {
    const t = setInterval(() => { now = Date.now(); }, 30_000);
    return () => clearInterval(t);
  });

  function formatRelative(date: Date | null): string {
    if (!date) return '';
    const diffMs = now - date.getTime();
    const diffS = Math.floor(diffMs / 1000);
    if (diffS < 60) return 'just now';
    const diffM = Math.floor(diffS / 60);
    if (diffM < 60) return `${diffM}m`;
    const h = Math.floor(diffM / 60);
    const m = diffM % 60;
    return m === 0 ? `${h}h` : `${h}h ${m}m`;
  }

  const tooltipText = $derived((() => {
    const t = formatRelative(conn.lastConnected);
    if (conn.state === 'connected') return t ? `Connected for ${t}` : 'Connected';
    if (conn.state === 'reconnecting') return t ? `Lost connection ${t} ago` : 'Reconnecting...';
    if (conn.state === 'disconnected') return t ? `Disconnected ${t} ago` : 'Disconnected';
    return 'Connecting...';
  })());
</script>

<Tooltip content={tooltipText}>
  {#if conn.state === 'connected'}
    <div class="flex items-center gap-2 px-2 py-1.5 text-xs text-success">
      <span class="h-2 w-2 shrink-0 rounded-full bg-success"></span>
      <span class="truncate">Live</span>
    </div>
  {:else if conn.state === 'reconnecting' || conn.state === 'connecting'}
    <div class="flex items-center gap-2 px-2 py-1.5 text-xs text-warning">
      <span class="h-2 w-2 shrink-0 rounded-full bg-warning animate-pulse"></span>
      <span class="truncate">{conn.state === 'reconnecting' ? 'Reconnecting...' : 'Connecting...'}</span>
    </div>
  {:else}
    <div class="flex items-center gap-2 px-2 py-1.5 text-xs text-destructive">
      <span class="h-2 w-2 shrink-0 rounded-full bg-destructive"></span>
      <span class="truncate">Connection lost</span>
    </div>
  {/if}
</Tooltip>

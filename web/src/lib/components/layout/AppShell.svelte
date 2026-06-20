<script lang="ts">
  import { type Snippet } from 'svelte';
  import Sidebar from './Sidebar.svelte';
  import ActivityFeedPanel from './ActivityFeedPanel.svelte';
  import HelpDrawer from '$lib/components/help/HelpDrawer.svelte';
  import { scanSessionStore, setScanSession, markScanSessionConnected, markScanSessionDisconnected } from '$lib/stores/scanSession.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { request } from '$lib/api/client.js';
  import { subscribeSSEType } from '$lib/api/sse.js';

  let { children }: { children: Snippet } = $props();

  let unsubSSE: (() => void) | null = null;

  const teamId = $derived(activeTeamStore.current?.teamId ?? '');

  // ── Mount check + SSE subscription ──────────────────────────────────────

  $effect(() => {
    const tid = teamId;
    if (!tid) {
      unsubSSE?.();
      unsubSSE = null;
      return;
    }

    // On team change, check for an existing session.
    request<{
      id: string;
      phone_connected: boolean;
    } | undefined>('GET', `/api/v1/teams/${tid}/scan-sessions/active`)
      .then((data) => {
        if (!data) return;
        setScanSession({
          id: data.id,
          teamId: tid,
          token: '',
          connected: data.phone_connected,
          liveConnected: data.phone_connected,
        });
      })
      .catch(() => {});

    // Subscribe to SSE for phone connect/disconnect events.
    unsubSSE?.();
    unsubSSE = subscribeSSEType(tid, 'scan_session_changed', (ev) => {
      const s = scanSessionStore.current;
      if (!s || ev.data.session_id !== s.id) return;
      if (ev.data.phone_connected) {
        markScanSessionConnected();
      } else {
        markScanSessionDisconnected();
      }
    });

    return () => {
      unsubSSE?.();
      unsubSSE = null;
    };
  });
</script>

<div class="flex h-screen overflow-hidden bg-background">
  <Sidebar />
  <main class="flex flex-1 flex-col overflow-hidden overflow-y-auto">
    {@render children()}
  </main>
  {#if scanSessionStore.current && teamId}
    <ActivityFeedPanel {teamId} />
  {/if}
</div>

<HelpDrawer />

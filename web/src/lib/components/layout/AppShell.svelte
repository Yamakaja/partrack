<script lang="ts">
  import { type Snippet } from 'svelte';
  import { Popover } from 'bits-ui';
  import Sidebar from './Sidebar.svelte';
  import { scanSessionStore, clearScanSession } from '$lib/stores/scanSession.svelte.js';
  import { request } from '$lib/api/client.js';
  import { Button } from '$lib/components/ui/button/index.js';

  let { children }: { children: Snippet } = $props();

  let badgeOpen = $state(false);

  async function disconnect() {
    const sess = scanSessionStore.current;
    if (!sess) return;
    badgeOpen = false;
    try {
      await request('DELETE', `/api/v1/teams/${sess.teamId}/scan-sessions/${sess.id}`);
    } catch {}
    clearScanSession();
  }

  const session = $derived(scanSessionStore.current);
</script>

<div class="flex h-screen overflow-hidden bg-background">
  <Sidebar />
  <div class="flex flex-1 flex-col overflow-hidden">

    <!-- Scanner badge bar (only visible when a scanner is connected) -->
    {#if session?.connected}
      <div class="flex items-center justify-end border-b border-border bg-background px-4 py-1.5 shrink-0">
        <Popover.Root
          open={badgeOpen}
          onOpenChange={(v) => { badgeOpen = v; }}
        >
          <Popover.Trigger>
            <button class="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-green-600 hover:bg-accent/50 dark:text-green-400">
              <span class="h-2 w-2 rounded-full bg-green-500"></span>
              Scanner connected
              <svg class="h-3 w-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
              </svg>
            </button>
          </Popover.Trigger>
          <Popover.Content
            class="z-50 w-56 rounded-md border border-border bg-background p-3 shadow-md"
            sideOffset={4}
            align="end"
          >
            <div class="space-y-2">
              <div>
                <p class="text-sm font-medium">Scanner connected</p>
                {#if session.orderRef}
                  <p class="text-xs text-muted-foreground mt-0.5">Receiving {session.orderRef}</p>
                {/if}
              </div>
              <Button variant="destructive" class="w-full" size="sm" onclick={disconnect}>
                Disconnect
              </Button>
            </div>
          </Popover.Content>
        </Popover.Root>
      </div>
    {/if}

    <main class="flex-1 overflow-y-auto">
      {@render children()}
    </main>
  </div>
</div>

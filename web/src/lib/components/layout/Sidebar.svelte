<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { currentRoute, linkAction } from '$lib/router/index.svelte.js';
  import { authStore, clearAuth } from '$lib/stores/auth.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  let activeTeam = $derived(activeTeamStore.current);
  import { logout } from '$lib/api/auth.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { toggleMode, mode } from 'mode-watcher';
  import { Sun, Moon } from 'lucide-svelte';
  import TeamSwitcher from './TeamSwitcher.svelte';
  import CommandPalette from './CommandPalette.svelte';
  import ScanModal from '$lib/components/scanner/ScanModal.svelte';
  import { paletteStore } from '$lib/stores/palette.svelte.js';
  import { versionStore } from '$lib/stores/version.svelte.js';
  import { createQuery } from '@tanstack/svelte-query';
  import { getCartCount } from '$lib/api/cart.js';
  import { getBulkScanBadgeCount } from '$lib/api/bulkScan.js';
  import { Popover } from 'bits-ui';
  import { scanSessionStore, clearScanSession } from '$lib/stores/scanSession.svelte.js';
  import SSEConnectionIndicator from './SSEConnectionIndicator.svelte';
  import PartrackMark from '$lib/components/brand/PartrackMark.svelte';
  import { Tooltip } from '$lib/components/ui/tooltip/index.js';
  import { request } from '$lib/api/client.js';

  const versionTooltip = $derived(
    versionStore.info
      ? `partrack ${versionStore.info.version} · commit ${versionStore.info.commit} · built ${versionStore.info.date}`
      : 'partrack'
  );

  function handleKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      paletteStore.open = true;
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleKeydown);
  });

  onDestroy(() => {
    window.removeEventListener('keydown', handleKeydown);
  });

  async function handleLogout() {
    await logout().catch(() => {});
    clearAuth();
    navigate('/login', true);
  }

  function isActive(path: string): boolean {
    return currentRoute().path === path || currentRoute().path.startsWith(path + '/');
  }

  function teamPath(sub: string): string {
    if (!activeTeam) return '#';
    return sub ? `/teams/${activeTeam.teamId}/${sub}` : `/teams/${activeTeam.teamId}`;
  }

  const isPurchaser = $derived(
    activeTeam?.role === 'purchaser' || activeTeam?.role === 'admin'
  );

  const cartCountQuery = createQuery(() => ({
    queryKey: ['cart', activeTeam?.teamId, 'count'],
    queryFn: () => getCartCount(activeTeam!.teamId),
    enabled: !!activeTeam && isPurchaser,
    staleTime: 30_000,
  }));

  const cartCount = $derived(cartCountQuery.data?.count ?? 0);

  const bulkScanBadgeQuery = createQuery(() => ({
    queryKey: ['bulk-scan-sessions', activeTeam?.teamId, 'badge'],
    queryFn: () => getBulkScanBadgeCount(activeTeam!.teamId),
    enabled: !!activeTeam,
    staleTime: 30_000,
  }));

  const bulkScanCount = $derived(bulkScanBadgeQuery.data?.count ?? 0);

  const session = $derived(scanSessionStore.current);
  const teamId = $derived(activeTeam?.teamId ?? '');
  let badgeOpen = $state(false);
  let scanModalOpen = $state(false);

  async function disconnect() {
    const sess = scanSessionStore.current;
    if (!sess) return;
    badgeOpen = false;
    try {
      await request('DELETE', `/api/v1/teams/${sess.teamId}/scan-sessions/${sess.id}`);
    } catch {}
    clearScanSession();
  }
</script>

<aside class="flex h-full w-60 shrink-0 flex-col border-r border-border bg-card">
  <!-- Header -->
  <div class="border-b border-border">
    <Tooltip content={versionTooltip} triggerClass="flex w-full items-center gap-2 px-4 py-3 text-left">
      <PartrackMark class="size-5 shrink-0 text-foreground" />
      <span class="font-semibold text-sm tracking-tight text-foreground">partrack</span>
    </Tooltip>
  </div>

  <!-- Team switcher -->
  <div class="border-b border-border px-3 py-2">
    <TeamSwitcher />
  </div>

  <!-- Nav -->
  {#if activeTeam}
    <nav data-tour="nav" class="flex-1 overflow-y-auto px-2 py-3 space-y-0.5 text-sm">
      <a href={teamPath('')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(`/teams/${activeTeam.teamId}`) && currentRoute().path === `/teams/${activeTeam.teamId}`
            ? 'bg-accent text-accent-foreground font-medium'
            : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Dashboard
      </a>

      <div class="mt-1 border-t border-border pt-3 pb-1 px-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">Catalog</div>

      <a href={teamPath('parts')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('parts')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Parts
      </a>
      <a href={teamPath('storage')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('storage')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Storage
      </a>
      <a href={teamPath('bulk-scan')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('bulk-scan')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Bulk Scan Review
        {#if bulkScanCount > 0}
          <span class="ml-auto text-xs font-medium tabular-nums bg-primary text-primary-foreground rounded-full px-1.5 py-0.5 leading-none">{bulkScanCount}</span>
        {/if}
      </a>

      <div class="mt-1 border-t border-border pt-3 pb-1 px-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">Projects</div>

      <a href={teamPath('projects')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('projects')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Projects
      </a>

      <div class="mt-1 border-t border-border pt-3 pb-1 px-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">Procurement</div>

      <a href={teamPath('cart')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('cart')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Cart
        {#if isPurchaser && cartCount > 0}
          <span class="ml-auto text-xs font-medium tabular-nums bg-primary text-primary-foreground rounded-full px-1.5 py-0.5 leading-none">{cartCount}</span>
        {/if}
      </a>
      <a href={teamPath('orders')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('orders')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Orders
      </a>

      <div class="mt-1 border-t border-border pt-3 pb-1 px-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">Assembly</div>

      <a href={teamPath('builds')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('builds')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Builds
      </a>
      <a href={teamPath('checkouts')} use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive(teamPath('checkouts')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Checkouts
      </a>

      {#if activeTeam.role === 'admin'}
        <div class="mt-1 border-t border-border pt-3 pb-1 px-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">Settings</div>
        <a href={teamPath('settings/members')} use:linkAction
          class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
            {isActive(teamPath('settings/members')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
          Members
        </a>
        <a href={teamPath('settings/distributors')} use:linkAction
          class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
            {isActive(teamPath('settings/distributors')) ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
          Distributors
        </a>
      {/if}
    </nav>
  {:else}
    <div class="flex-1"></div>
  {/if}

  <!-- Administration (instance admins only) -->
  {#if authStore.user?.is_admin}
    <div class="border-t border-border px-2 py-3 space-y-0.5 text-sm">
      <div class="pb-1 px-2 text-xs font-medium text-muted-foreground uppercase tracking-wider">Administration</div>
      <a href="/admin/users" use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive('/admin/users') ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Users
      </a>
      <a href="/admin/parameters" use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive('/admin/parameters') ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Parameters
      </a>
      <a href="/admin/category-parameters" use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive('/admin/category-parameters') ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        Category Parameters
      </a>
      <a href="/admin/ldap" use:linkAction
        class="flex items-center gap-2 rounded px-2 py-1.5 transition-colors
          {isActive('/admin/ldap') ? 'bg-accent text-accent-foreground font-medium' : 'text-foreground/80 hover:text-foreground hover:bg-accent/50'}">
        LDAP
      </a>
    </div>
  {/if}

  <!-- Connection status: live server link + phone scanner -->
  {#if teamId}
    <div data-tour="status">
      <div class="border-t border-border px-2 py-0.5">
        <SSEConnectionIndicator />
      </div>

      <div class="border-t border-border px-2 py-1.5">
      {#if !session}
        <button
          onclick={() => { scanModalOpen = true; }}
          class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-xs text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
        >
          <svg class="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M12 18h.01M8 21h8a2 2 0 002-2V5a2 2 0 00-2-2H8a2 2 0 00-2 2v14a2 2 0 002 2z" />
          </svg>
          Connect Phone
        </button>

      {:else}
        <Popover.Root open={badgeOpen} onOpenChange={(v) => { badgeOpen = v; }}>
          <Popover.Trigger>
            <button class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-xs hover:bg-accent/50 transition-colors
              {session.liveConnected ? 'text-success' : 'text-muted-foreground'}">
              <span class="h-2 w-2 shrink-0 rounded-full {session.liveConnected ? 'bg-success' : 'bg-zinc-400'}"></span>
              <span class="truncate">{session.liveConnected ? 'Scanner connected' : 'Scanner disconnected'}</span>
            </button>
          </Popover.Trigger>
          <Popover.Content
            class="z-50 w-56 rounded-md border border-border bg-background p-3 shadow-md"
            sideOffset={6}
            side="right"
            align="end"
          >
            <div class="space-y-2">
              {#if session.liveConnected}
                <p class="text-sm font-medium">Scanner connected</p>
                <button
                  onclick={disconnect}
                  class="w-full rounded-md bg-destructive px-2 py-1.5 text-xs font-medium text-destructive-foreground hover:bg-destructive/90"
                >
                  Disconnect
                </button>
              {:else}
                <p class="text-sm font-medium">Scanner disconnected</p>
                <p class="text-xs text-muted-foreground">Reconnect by scanning a new QR code.</p>
                <div class="flex gap-2">
                  <button
                    onclick={() => { badgeOpen = false; scanModalOpen = true; }}
                    class="flex-1 rounded-md border border-border px-2 py-1.5 text-xs font-medium hover:bg-accent/50"
                  >
                    Reconnect
                  </button>
                  <button
                    onclick={disconnect}
                    class="flex-1 rounded-md bg-destructive px-2 py-1.5 text-xs font-medium text-destructive-foreground hover:bg-destructive/90"
                  >
                    Dismiss
                  </button>
                </div>
              {/if}
            </div>
          </Popover.Content>
        </Popover.Root>
      {/if}
      </div>
    </div>
  {/if}

  <!-- User footer -->
  <div data-tour="account" class="border-t border-border px-3 py-2 flex items-center gap-2">
    <div class="flex-1 min-w-0">
      <p class="text-sm font-medium truncate">{authStore.user?.display_name ?? ''}</p>
      <a href="/settings/profile" use:linkAction class="text-xs text-muted-foreground hover:underline truncate block">
        Account
      </a>
    </div>
    <button onclick={toggleMode} title="Toggle theme" class="text-muted-foreground hover:text-foreground p-1 rounded hover:bg-accent/50">
      {#if mode.current === 'dark'}
        <Sun class="size-4" />
      {:else}
        <Moon class="size-4" />
      {/if}
    </button>
    <button onclick={handleLogout} class="text-xs text-muted-foreground hover:text-foreground px-2 py-1 rounded hover:bg-accent/50">
      Log out
    </button>
  </div>
</aside>

<CommandPalette />

{#if scanModalOpen && teamId}
  <ScanModal {teamId} bind:open={scanModalOpen} />
{/if}

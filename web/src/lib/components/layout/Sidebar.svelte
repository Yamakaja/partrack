<script lang="ts">
  import { currentRoute, linkAction } from '$lib/router/index.svelte.js';
  import { authStore, clearAuth } from '$lib/stores/auth.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  let activeTeam = $derived(activeTeamStore.current);
  import { logout } from '$lib/api/auth.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { toggleMode, mode } from 'mode-watcher';
  import { Sun, Moon } from 'lucide-svelte';
  import TeamSwitcher from './TeamSwitcher.svelte';

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
</script>

<aside class="flex h-full w-60 shrink-0 flex-col border-r border-border bg-card">
  <!-- Header -->
  <div class="flex items-center gap-2 border-b border-border px-4 py-3">
    <span class="font-semibold text-sm tracking-tight text-foreground">partrack</span>
  </div>

  <!-- Team switcher -->
  <div class="border-b border-border px-3 py-2">
    <TeamSwitcher />
  </div>

  <!-- Nav -->
  {#if activeTeam}
    <nav class="flex-1 overflow-y-auto px-2 py-3 space-y-0.5 text-sm">
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
    </div>
  {/if}

  <!-- User footer -->
  <div class="border-t border-border px-3 py-2 flex items-center gap-2">
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

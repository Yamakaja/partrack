<script lang="ts">
  import { onMount, type Component } from 'svelte';
  import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
  import { currentRoute, linkAction, matchRoute, navigate, type RouteParams } from '$lib/router/index.svelte.js';
  import { initAuth, authStore } from '$lib/stores/auth.svelte.js';
  import { setActiveTeam, restoreActiveTeam, activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import { openForTeam, closeSSE, setReconnectHandler } from '$lib/api/sse.js';
  import {
    initCacheInvalidation,
    activateTeamInvalidation,
    deactivateTeamInvalidation,
    invalidateAll,
  } from '$lib/cache/invalidation.js';
  import { ModeWatcher } from 'mode-watcher';
  import AppShell from '$lib/components/layout/AppShell.svelte';
  import Toaster from '$lib/components/common/Toaster.svelte';

  import Login from './routes/Login.svelte';
  import TeamSelect from './routes/TeamSelect.svelte';
  import Dashboard from './routes/team/Dashboard.svelte';
  import Parts from './routes/team/Parts.svelte';
  import PartDetail from './routes/team/PartDetail.svelte';
  import Storage from './routes/team/Storage.svelte';
  import Projects from './routes/team/Projects.svelte';
  import ProjectDetail from './routes/team/ProjectDetail.svelte';
  import RevisionDetail from './routes/team/RevisionDetail.svelte';
  import Cart from './routes/team/Cart.svelte';
  import Orders from './routes/team/Orders.svelte';
  import OrderDetail from './routes/team/OrderDetail.svelte';
  import Builds from './routes/team/Builds.svelte';
  import BuildDetail from './routes/team/BuildDetail.svelte';
  import Checkouts from './routes/team/Checkouts.svelte';
  import TeamSettings from './routes/team/settings/Team.svelte';
  import Members from './routes/team/settings/Members.svelte';
  import Distributors from './routes/team/settings/Distributors.svelte';
  import Profile from './routes/settings/Profile.svelte';
  import Sessions from './routes/settings/Sessions.svelte';
  import Tokens from './routes/settings/Tokens.svelte';
  import AdminUsers from './routes/admin/Users.svelte';
  import AdminParameters from './routes/admin/Parameters.svelte';
  import AdminLdap from './routes/admin/Ldap.svelte';
  import ResetPassword from './routes/admin/ResetPassword.svelte';
  import Scan from './routes/Scan.svelte';
  import BulkScan from './routes/team/BulkScan.svelte';
  import BulkScanReview from './routes/team/BulkScanReview.svelte';

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { staleTime: 30_000, retry: 1 },
    },
  });

  initCacheInvalidation(queryClient);
  setReconnectHandler(() => { invalidateAll(); });

  // Route table: ordered most-specific first.
  const ROUTES = [
    { pattern: '/login',                                                                  component: Login },
    { pattern: '/reset-password',                                                         component: ResetPassword },
    { pattern: '/scan',                                                                   component: Scan },
    { pattern: '/admin/parameters',                                                       component: AdminParameters },
    { pattern: '/admin/ldap',                                                             component: AdminLdap },
    { pattern: '/admin/users',                                                            component: AdminUsers },
    { pattern: '/admin',                                                                  component: AdminUsers },
    { pattern: '/settings/profile',                                                       component: Profile },
    { pattern: '/settings/sessions',                                                      component: Sessions },
    { pattern: '/settings/tokens',                                                        component: Tokens },
    { pattern: '/teams/:teamId/parts/:partId',                                            component: PartDetail },
    { pattern: '/teams/:teamId/parts',                                                    component: Parts },
    { pattern: '/teams/:teamId/storage/:locationId',                                       component: Storage },
    { pattern: '/teams/:teamId/storage',                                                  component: Storage },
    { pattern: '/teams/:teamId/projects/:projectId/revisions/:revId',                    component: RevisionDetail },
    { pattern: '/teams/:teamId/projects/:projectId',                                      component: ProjectDetail },
    { pattern: '/teams/:teamId/projects',                                                 component: Projects },
    { pattern: '/teams/:teamId/cart',                                                     component: Cart },
    { pattern: '/teams/:teamId/orders/:orderId',                                          component: OrderDetail },
    { pattern: '/teams/:teamId/orders',                                                   component: Orders },
    { pattern: '/teams/:teamId/builds/:buildId',                                          component: BuildDetail },
    { pattern: '/teams/:teamId/builds',                                                   component: Builds },
    { pattern: '/teams/:teamId/checkouts',                                                component: Checkouts },
    { pattern: '/teams/:teamId/bulk-scan/:sessionId',                                     component: BulkScanReview },
    { pattern: '/teams/:teamId/bulk-scan',                                                component: BulkScan },
    { pattern: '/teams/:teamId/settings/members',                                         component: Members },
    { pattern: '/teams/:teamId/settings/distributors',                                    component: Distributors },
    { pattern: '/teams/:teamId/settings',                                                 component: TeamSettings },
    { pattern: '/teams/:teamId',                                                          component: Dashboard },
    { pattern: '/',                                                                        component: TeamSelect },
  ];

  function resolve(path: string) {
    for (const route of ROUTES) {
      const params = matchRoute(route.pattern, path);
      if (params !== null) return { component: route.component, params };
    }
    return null;
  }

  $effect(() => {
    const route = currentRoute();

    // Redirect legacy inventory routes to the unified parts view.
    const invMatch = matchRoute('/teams/:teamId/inventory', route.path)
      ?? matchRoute('/teams/:teamId/inventory/:entryId', route.path);
    if (invMatch) {
      navigate(`/teams/${invMatch.teamId}/parts`, true);
      return;
    }

    const r = resolve(route.path);
    const routeTeamId = r?.params?.teamId;
    // A team route updates the active selection; team-less routes (admin/
    // settings/root) leave the persisted selection intact so the team
    // switcher and team nav stay populated.
    if (routeTeamId) {
      setActiveTeam(routeTeamId);
    }

    // SSE + cache invalidation follow the active team, not the route, so live
    // updates keep working while on admin/settings pages.
    const tid = activeTeamStore.current?.teamId;
    if (tid) {
      openForTeam(tid);
      activateTeamInvalidation(tid);
    } else {
      closeSSE();
      deactivateTeamInvalidation();
    }

    const publicPaths = ['/login', '/reset-password', '/scan'];
    if (!authStore.loading && !authStore.user && !publicPaths.includes(route.path)) {
      navigate('/login', true);
    }
  });

  onMount(async () => {
    await initAuth();
    // Restore the persisted team so deep-links / refreshes onto team-less
    // routes (admin/settings) still show the last-selected team and its nav.
    if (authStore.user) restoreActiveTeam();
    if (authStore.user && currentRoute().path === '/login') {
      navigate('/', true);
    }
  });

  let resolved = $derived(resolve(currentRoute().path));
  let isShellless = $derived(['/login', '/reset-password', '/scan'].includes(currentRoute().path));
</script>

<QueryClientProvider client={queryClient}>
  <ModeWatcher />
  {#if authStore.loading}
    <div class="flex h-screen items-center justify-center bg-background">
      <div class="text-muted-foreground text-sm">Loading…</div>
    </div>
  {:else if resolved}
    {@const Cmp = resolved.component as Component<RouteParams>}
    {#if isShellless}
      <Cmp {...resolved.params} />
    {:else}
      <AppShell>
        <Cmp {...resolved.params} />
      </AppShell>
    {/if}
  {:else}
    <div class="flex h-screen flex-col items-center justify-center gap-2">
      <p class="text-lg font-medium">404 — Page not found</p>
      <a use:linkAction href="/" class="text-primary text-sm underline">Go home</a>
    </div>
  {/if}

  {#if !isShellless}<Toaster />{/if}
</QueryClientProvider>

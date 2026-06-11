<script lang="ts">
  import { onMount } from 'svelte';
  import { login, getMe, getAuthProviders, type OIDCProvider } from '$lib/api/auth.js';
  import { setAuth } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';

  let email = $state('');
  let password = $state('');
  let loading = $state(false);
  let error = $state('');
  let showScannerLink = $state(false);
  let providers = $state<OIDCProvider[]>([]);

  // Error codes the OIDC callback redirects back with (?error=CODE).
  const ssoErrors: Record<string, string> = {
    INVALID_STATE: 'Sign-in attempt expired or was invalid. Please try again.',
    OIDC_EXCHANGE_FAILED: 'Single sign-on failed while contacting the identity provider.',
    OIDC_NO_ID_TOKEN: 'The identity provider did not return an ID token.',
    OIDC_VERIFY_FAILED: 'The identity provider response could not be verified.',
    OIDC_CLAIMS_ERROR: 'The identity provider response could not be read.',
    OIDC_NO_EMAIL: 'Your identity provider account has no email address.',
    OIDC_EMAIL_UNVERIFIED: 'Your email address is not verified with the identity provider.',
    OIDC_PROVIDER_UNAVAILABLE: 'The identity provider is currently unavailable. Please try again later.',
    PROVISIONING_DISABLED: 'No account exists for this identity and self-registration is disabled. Contact an administrator.',
    ACCOUNT_INACTIVE: 'Your account is inactive. Contact an administrator.',
    INTERNAL_ERROR: 'Single sign-on failed due to a server error. Please try again.',
  };

  onMount(() => {
    const token = localStorage.getItem('scan_token');
    const isMobile = navigator.maxTouchPoints > 0 && /Mobi|Android|iPhone/i.test(navigator.userAgent);
    showScannerLink = !!(token && isMobile);

    const code = new URLSearchParams(window.location.search).get('error');
    if (code) {
      error = ssoErrors[code] ?? 'Sign-in failed. Please try again.';
      history.replaceState(null, '', '/login');
    }

    getAuthProviders()
      .then((res) => (providers = res.providers))
      .catch(() => {}); // login page must still work if this fails
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
    loading = true;
    error = '';
    try {
      await login(email, password);
      const me = await getMe();
      setAuth(me.user, me.memberships);
      navigate('/', true);
    } catch (err: any) {
      error = err.message ?? 'Login failed';
    } finally {
      loading = false;
    }
  }
</script>

<div class="flex min-h-screen items-center justify-center bg-background">
  <div class="w-full max-w-sm">
    <div class="space-y-6 rounded-lg border border-border bg-card p-8 shadow-sm">
      <div>
        <h1 class="text-2xl font-bold">partrack</h1>
        <p class="mt-1 text-sm text-muted-foreground">Sign in to your account</p>
      </div>

      <form onsubmit={handleSubmit} class="space-y-4">
        {#if error}
          <div class="rounded-md bg-destructive/10 border border-destructive/30 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        {/if}

        <div class="space-y-1">
          <label for="email" class="text-sm font-medium">Email</label>
          <input
            id="email"
            type="email"
            bind:value={email}
            required
            autocomplete="email"
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <div class="space-y-1">
          <label for="password" class="text-sm font-medium">Password</label>
          <input
            id="password"
            type="password"
            bind:value={password}
            required
            autocomplete="current-password"
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <button
          type="submit"
          disabled={loading}
          class="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-colors"
        >
          {loading ? 'Signing in…' : 'Sign in'}
        </button>
      </form>

      {#if providers.length > 0}
        <div class="space-y-3">
          <div class="relative flex items-center">
            <div class="flex-1 border-t border-border"></div>
            <span class="mx-3 text-xs text-muted-foreground">or</span>
            <div class="flex-1 border-t border-border"></div>
          </div>
          {#each providers as provider (provider.name)}
            <a
              href={`/api/v1/auth/oidc/${encodeURIComponent(provider.name)}`}
              class="block w-full rounded-md border border-border bg-card px-4 py-2 text-center text-sm font-medium hover:bg-accent transition-colors"
            >
              Continue with {provider.display_name}
            </a>
          {/each}
        </div>
      {/if}
    </div>

    {#if showScannerLink}
      <div class="mt-4">
        <div class="relative flex items-center mb-4">
          <div class="flex-1 border-t border-border"></div>
          <span class="mx-3 text-xs text-muted-foreground">or</span>
          <div class="flex-1 border-t border-border"></div>
        </div>
        <p class="text-xs text-muted-foreground text-center mb-2">Scanner session detected</p>
        <button
          onclick={() => navigate('/scan', true)}
          class="w-full rounded-md border border-border bg-card px-4 py-2 text-sm font-medium hover:bg-accent transition-colors"
        >
          Open Scanner
        </button>
      </div>
    {/if}
  </div>
</div>

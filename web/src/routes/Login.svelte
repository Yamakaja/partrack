<script lang="ts">
  import { login, getMe } from '$lib/api/auth.js';
  import { setAuth } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

  let email = $state('');
  let password = $state('');
  let loading = $state(false);
  let error = $state('');

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
  <div class="w-full max-w-sm space-y-6 rounded-lg border border-border bg-card p-8 shadow-sm">
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
  </div>
</div>

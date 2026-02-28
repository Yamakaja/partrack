<script lang="ts">
  import { onMount } from 'svelte';
  import { getResetPasswordInfo, completePasswordReset } from '$lib/api/admin.js';
  import { setAuth } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';

  let pageState = $state<'loading' | 'ready' | 'invalid' | 'done'>('loading');
  let displayName = $state('');
  let email = $state('');
  let expiresAt = $state('');
  let password = $state('');
  let confirm = $state('');
  let submitting = $state(false);
  let error = $state('');
  let token = $state('');

  onMount(async () => {
    const params = new URLSearchParams(window.location.search);
    token = params.get('token') ?? '';
    if (!token) {
      pageState = 'invalid';
      return;
    }
    try {
      const info = await getResetPasswordInfo(token);
      displayName = info.display_name;
      email = info.email;
      expiresAt = new Date(info.expires_at).toLocaleString();
      pageState = 'ready';
    } catch {
      pageState = 'invalid';
    }
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
    error = '';
    if (password !== confirm) {
      error = 'Passwords do not match';
      return;
    }
    submitting = true;
    try {
      const res = await completePasswordReset(token, password);
      setAuth(res.user, res.memberships);
      navigate('/', true);
    } catch (err: any) {
      error = err.message ?? 'Failed to reset password';
    } finally {
      submitting = false;
    }
  }
</script>

<div class="flex min-h-screen items-center justify-center bg-background">
  <div class="w-full max-w-sm space-y-6 rounded-lg border border-border bg-card p-8 shadow-sm">
    <div>
      <h1 class="text-2xl font-bold">partrack</h1>
      <p class="mt-1 text-sm text-muted-foreground">Set your password</p>
    </div>

    {#if pageState === 'loading'}
      <p class="text-sm text-muted-foreground">Verifying link…</p>
    {:else if pageState === 'invalid'}
      <div class="rounded-md bg-destructive/10 border border-destructive/30 px-3 py-2 text-sm text-destructive">
        This link is invalid or has already been used.
      </div>
      <a href="/login" class="text-sm text-primary underline">Back to login</a>
    {:else}
      <p class="text-sm text-muted-foreground">
        Setting password for <span class="font-medium text-foreground">{displayName}</span> ({email}).
        Link expires {expiresAt}.
      </p>

      <form onsubmit={handleSubmit} class="space-y-4">
        {#if error}
          <div class="rounded-md bg-destructive/10 border border-destructive/30 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        {/if}

        <div class="space-y-1">
          <label for="password" class="text-sm font-medium">New password</label>
          <input
            id="password"
            type="password"
            bind:value={password}
            required
            autocomplete="new-password"
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <div class="space-y-1">
          <label for="confirm" class="text-sm font-medium">Confirm password</label>
          <input
            id="confirm"
            type="password"
            bind:value={confirm}
            required
            autocomplete="new-password"
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <button
          type="submit"
          disabled={submitting}
          class="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50 transition-colors"
        >
          {submitting ? 'Setting password…' : 'Set password & sign in'}
        </button>
      </form>
    {/if}
  </div>
</div>

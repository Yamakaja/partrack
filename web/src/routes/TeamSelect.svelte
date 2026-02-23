<script lang="ts">
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { onMount } from 'svelte';

  onMount(() => {
    if (authStore.memberships.length === 1) {
      navigate(`/teams/${authStore.memberships[0].team_id}`, true);
    }
  });
</script>

<div class="p-8 max-w-2xl mx-auto">
  <h1 class="text-2xl font-bold mb-1">Your teams</h1>
  <p class="text-sm text-muted-foreground mb-6">Select a team to get started.</p>

  <div class="grid gap-3 sm:grid-cols-2">
    {#each authStore.memberships as m}
      <a href="/teams/{m.team_id}" use:linkAction
        class="block rounded-lg border border-border bg-card p-4 hover:bg-accent/50 transition-colors">
        <p class="font-medium">{m.team_name}</p>
        <p class="text-xs text-muted-foreground mt-0.5 capitalize">{m.role}</p>
      </a>
    {/each}

    {#if authStore.memberships.length === 0}
      <p class="text-muted-foreground text-sm col-span-2">You're not a member of any teams yet.</p>
    {/if}
  </div>
</div>

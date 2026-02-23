<script lang="ts">
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Input } from '$lib/components/ui/input/index.js';

  // No updateProfile function exists in the API; display read-only profile info.
  let user = $derived(authStore.user);
</script>

<div class="flex flex-col h-full">
  <Topbar title="Profile" />

  <div class="flex-1 overflow-auto p-6">
    {#if authStore.loading}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if user}
      <div class="space-y-4 max-w-md">
        <div class="space-y-1">
          <Label for="profile-name">Display Name</Label>
          <Input
            id="profile-name"
            value={user.display_name}
            disabled
            class="bg-muted cursor-not-allowed"
          />
        </div>
        <div class="space-y-1">
          <Label for="profile-email">Email</Label>
          <Input
            id="profile-email"
            value={user.email}
            disabled
            class="bg-muted cursor-not-allowed"
          />
        </div>
        <p class="text-xs text-muted-foreground">
          Profile editing is not yet available. Contact your administrator to update profile details.
        </p>
      </div>
    {:else}
      <div class="text-sm text-muted-foreground">Not logged in.</div>
    {/if}
  </div>
</div>

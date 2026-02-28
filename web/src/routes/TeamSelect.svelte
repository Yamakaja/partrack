<script lang="ts">
  import { authStore, initAuth } from '$lib/stores/auth.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { onMount } from 'svelte';
  import { createTeam } from '$lib/api/teams.js';
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus } from 'lucide-svelte';

  onMount(() => {
    if (authStore.memberships.length === 1) {
      navigate(`/teams/${authStore.memberships[0].team_id}`, true);
    }
  });

  let newTeamOpen = $state(false);
  let newTeamName = $state('');
  let newTeamDesc = $state('');
  let creating = $state(false);

  async function handleCreate(e: SubmitEvent) {
    e.preventDefault();
    if (!newTeamName.trim()) return;
    creating = true;
    try {
      const team = await createTeam(newTeamName.trim(), newTeamDesc.trim() || undefined);
      await initAuth();
      newTeamOpen = false;
      newTeamName = '';
      newTeamDesc = '';
      navigate(`/teams/${team.id}`);
    } catch {
      pushToast('error', 'Failed to create team');
    } finally {
      creating = false;
    }
  }
</script>

<div class="p-8 max-w-2xl mx-auto">
  <div class="flex items-center justify-between mb-1">
    <h1 class="text-2xl font-bold">Your teams</h1>
    {#if authStore.user?.is_admin}
      <Button onclick={() => { newTeamOpen = true; }} size="sm">
        <Plus class="h-4 w-4 mr-1" /> New Team
      </Button>
    {/if}
  </div>
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

{#if authStore.user?.is_admin}
<Dialog.Root bind:open={newTeamOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">New Team</Dialog.Title>
      <form onsubmit={handleCreate} class="space-y-3">
        <div class="space-y-1">
          <Label for="team-name">Name <span class="text-destructive">*</span></Label>
          <Input id="team-name" bind:value={newTeamName} placeholder="e.g. Hardware Team" required />
        </div>
        <div class="space-y-1">
          <Label for="team-desc">Description</Label>
          <Input id="team-desc" bind:value={newTeamDesc} placeholder="Optional" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { newTeamOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={creating || !newTeamName.trim()}>
            {creating ? 'Creating…' : 'Create Team'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
{/if}

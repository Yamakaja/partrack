<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getTeam, updateTeam } from '$lib/api/teams.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let teamQuery = createQuery(() => ({
    queryKey: ['team', teamId],
    queryFn: () => getTeam(teamId),
  }));

  let name = $state('');
  let description = $state('');
  let saving = $state(false);
  let initialized = $state(false);

  $effect(() => {
    const data = teamQuery.data;
    if (data && !initialized) {
      name = data.name;
      description = data.description ?? '';
      initialized = true;
    }
  });

  async function handleSave(e: Event) {
    e.preventDefault();
    if (!name.trim()) return;
    saving = true;
    try {
      await updateTeam(teamId, {
        name: name.trim(),
        description: description.trim() || undefined,
      });
      qc.invalidateQueries({ queryKey: ['team', teamId] });
      pushToast('success', 'Team settings saved');
    } catch (err: any) {
      pushToast('error', 'Failed to save settings', err.message);
    } finally {
      saving = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Team Settings" />

  <div class="flex-1 overflow-auto p-6">
    {#if teamQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if teamQuery.isError}
      <div class="text-sm text-destructive">Error: {teamQuery.error.message}</div>
    {:else if teamQuery.data}
      <form onsubmit={handleSave} class="space-y-4 max-w-md">
        <div class="space-y-1">
          <Label for="team-name">Name <span class="text-destructive">*</span></Label>
          <Input
            id="team-name"
            bind:value={name}
            placeholder="Team name"
            required
          />
        </div>
        <div class="space-y-1">
          <Label for="team-desc">Description</Label>
          <Input
            id="team-desc"
            bind:value={description}
            placeholder="Optional description"
          />
        </div>
        <div class="pt-2">
          <Button type="submit" disabled={saving || !name.trim()}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    {/if}
  </div>
</div>

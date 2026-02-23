<script lang="ts">
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  let activeTeam = $derived(activeTeamStore.current);
  import { navigate } from '$lib/router/index.svelte.js';

  let open = $state(false);

  function selectTeam(teamId: string) {
    open = false;
    navigate(`/teams/${teamId}`);
  }

  let currentTeamName = $derived(
    activeTeam
      ? (authStore.memberships.find(m => m.team_id === activeTeam!.teamId)?.team_name ?? 'Unknown team')
      : 'Select team'
  );
</script>

<div class="relative">
  <button
    onclick={() => open = !open}
    class="w-full flex items-center justify-between gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent/50 transition-colors"
  >
    <span class="truncate font-medium text-foreground">{currentTeamName}</span>
    <span class="text-muted-foreground text-xs">▾</span>
  </button>

  {#if open}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="absolute left-0 top-full mt-1 z-10 w-full rounded-md border border-border bg-popover shadow-md py-1"
      onkeydown={(e) => e.key === 'Escape' && (open = false)}
    >
      {#each authStore.memberships as m}
        <button
          class="w-full px-3 py-1.5 text-left text-sm hover:bg-accent transition-colors
            {m.team_id === activeTeam?.teamId ? 'font-medium text-foreground' : 'text-muted-foreground'}"
          onclick={() => selectTeam(m.team_id)}
        >
          {m.team_name}
        </button>
      {/each}
    </div>

    <!-- Backdrop to close dropdown -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="fixed inset-0 z-[9]" onclick={() => open = false} onkeydown={() => {}}></div>
  {/if}
</div>
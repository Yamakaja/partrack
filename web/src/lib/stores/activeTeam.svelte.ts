import type { Role } from '$lib/router/guards.js';
import { authStore } from './auth.svelte.js';

interface ActiveTeamState {
  teamId: string;
  role: Role;
}

// Wrapped in an object so properties can be mutated (Svelte 5 export constraint).
export const activeTeamStore = $state<{ current: ActiveTeamState | null }>({ current: null });

/** Convenience getter — use in templates. */
export function activeTeam(): ActiveTeamState | null {
  return activeTeamStore.current;
}

export function setActiveTeam(teamId: string) {
  const membership = authStore.memberships.find(m => m.team_id === teamId);
  if (membership) {
    activeTeamStore.current = { teamId, role: membership.role as Role };
  } else {
    activeTeamStore.current = null;
  }
}

export function clearActiveTeam() {
  activeTeamStore.current = null;
}

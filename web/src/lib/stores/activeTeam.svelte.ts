import type { Role } from '$lib/router/guards.js';
import { authStore } from './auth.svelte.js';

interface ActiveTeamState {
  teamId: string;
  role: Role;
}

const STORAGE_KEY = 'partrack.activeTeam';

// Wrapped in an object so properties can be mutated (Svelte 5 export constraint).
export const activeTeamStore = $state<{ current: ActiveTeamState | null }>({ current: null });

/** Convenience getter — use in templates. */
export function activeTeam(): ActiveTeamState | null {
  return activeTeamStore.current;
}

export function setActiveTeam(teamId: string) {
  const membership = authStore.memberships.find(m => m.team_id === teamId);
  if (membership) {
    const role = membership.role as Role;
    // Only reassign when the value actually changes — reassigning a fresh
    // object on every call would retrigger effects that read this store.
    const cur = activeTeamStore.current;
    if (!cur || cur.teamId !== teamId || cur.role !== role) {
      activeTeamStore.current = { teamId, role };
    }
    try {
      localStorage.setItem(STORAGE_KEY, teamId);
    } catch {}
  } else {
    clearActiveTeam();
  }
}

export function clearActiveTeam() {
  activeTeamStore.current = null;
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {}
}

/**
 * Restore the persisted team selection, validating it against the current
 * memberships. Used so the team stays selected across team-less routes
 * (admin/settings) and hard refreshes / deep-links. No-op if the stored team
 * is gone or the user is no longer a member.
 */
export function restoreActiveTeam() {
  if (activeTeamStore.current) return;
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(STORAGE_KEY);
  } catch {}
  if (stored) setActiveTeam(stored);
}

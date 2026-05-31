import type { User, TeamMembership } from '$lib/types/auth.js';
import { getMe } from '$lib/api/auth.js';
import { clearActiveTeam } from './activeTeam.svelte.js';

interface AuthState {
  user: User | null;
  memberships: TeamMembership[];
  loading: boolean;
}

export const authStore = $state<AuthState>({
  user: null,
  memberships: [],
  loading: true,
});

export async function initAuth() {
  authStore.loading = true;
  try {
    const data = await getMe();
    authStore.user = data.user;
    authStore.memberships = data.memberships;
  } catch {
    authStore.user = null;
    authStore.memberships = [];
  } finally {
    authStore.loading = false;
  }
}

export function setAuth(user: User, memberships: TeamMembership[]) {
  authStore.user = user;
  authStore.memberships = memberships;
  authStore.loading = false;
}

export function clearAuth() {
  authStore.user = null;
  authStore.memberships = [];
  authStore.loading = false;
  clearActiveTeam();
}

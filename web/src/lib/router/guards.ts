import { navigate } from './index.svelte.js';
import { authStore } from '$lib/stores/auth.svelte.js';

export type Role = 'viewer' | 'member' | 'purchaser' | 'admin';

const ROLE_ORDER: Role[] = ['viewer', 'member', 'purchaser', 'admin'];

export function roleIndex(role: Role): number {
  return ROLE_ORDER.indexOf(role);
}

export function hasMinRole(userRole: Role, minRole: Role): boolean {
  return roleIndex(userRole) >= roleIndex(minRole);
}

/** Returns true if the user is authenticated; redirects to /login if not. */
export function requireAuth(): boolean {
  if (authStore.loading) return false; // wait for init
  if (!authStore.user) {
    navigate('/login', true);
    return false;
  }
  return true;
}

/**
 * Returns true if the user has at least minRole in the given team.
 * Redirects to /teams/:teamId if not.
 */
export function requireRole(teamId: string, minRole: Role): boolean {
  if (!requireAuth()) return false;
  const membership = authStore.memberships.find(m => m.team_id === teamId);
  if (!membership || !hasMinRole(membership.role as Role, minRole)) {
    navigate(`/teams/${teamId}`, true);
    return false;
  }
  return true;
}

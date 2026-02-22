import type { Role } from '$lib/router/guards.js';

const ROLE_ORDER: Role[] = ['viewer', 'member', 'purchaser', 'admin'];

export function roleIndex(role: string): number {
  return ROLE_ORDER.indexOf(role as Role);
}

export function hasMinRole(userRole: string, minRole: Role): boolean {
  return roleIndex(userRole) >= roleIndex(minRole);
}

export const ROLE_LABELS: Record<string, string> = {
  viewer: 'Viewer',
  member: 'Member',
  purchaser: 'Purchaser',
  admin: 'Admin',
};

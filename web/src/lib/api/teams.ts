import { request } from './client.js';
import type { Team, TeamMembership } from '$lib/types/auth.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface TeamMember {
  user_id: string;
  display_name: string;
  email: string;
  role: string;
  joined_at: string;
}

export function listTeams() {
  return request<PaginatedResponse<Team>>('GET', '/api/v1/teams');
}

export function createTeam(name: string, description?: string) {
  return request<Team>('POST', '/api/v1/teams', { name, description });
}

export function getTeam(teamId: string) {
  return request<Team>('GET', `/api/v1/teams/${teamId}`);
}

export function updateTeam(teamId: string, body: { name?: string; description?: string }) {
  return request<Team>('PATCH', `/api/v1/teams/${teamId}`, body);
}

export function listMembers(teamId: string) {
  return request<PaginatedResponse<TeamMember>>('GET', `/api/v1/teams/${teamId}/members`);
}

export function addMember(teamId: string, user_id: string, role: string) {
  return request<TeamMember>('POST', `/api/v1/teams/${teamId}/members`, { user_id, role });
}

export function updateMemberRole(teamId: string, userId: string, role: string) {
  return request<TeamMember>('PATCH', `/api/v1/teams/${teamId}/members/${userId}`, { role });
}

export function removeMember(teamId: string, userId: string) {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/members/${userId}`);
}

export interface UserSearchResult {
  id: string;
  display_name: string;
  email: string;
}

export async function searchUsers(q: string): Promise<UserSearchResult[]> {
  const res = await request<{ users: UserSearchResult[] }>('GET', `/api/v1/users/search?q=${encodeURIComponent(q)}`);
  return res.users ?? [];
}

import { request } from './client.js';
import type { User, TeamMembership, Session, ApiToken, ApiTokenCreated } from '$lib/types/auth.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export interface MeResponse {
  user: User;
  memberships: TeamMembership[];
}

export function getMe() {
  return request<MeResponse>('GET', '/api/v1/auth/me');
}

export function login(email: string, password: string) {
  return request<{ user: User }>('POST', '/api/v1/auth/login', { email, password });
}

export function logout() {
  return request<void>('POST', '/api/v1/auth/logout');
}

export function listSessions() {
  return request<PaginatedResponse<Session>>('GET', '/api/v1/sessions');
}

export function revokeSession(sessionId: string) {
  return request<void>('DELETE', `/api/v1/sessions/${sessionId}`);
}

export function listTokens() {
  return request<PaginatedResponse<ApiToken>>('GET', '/api/v1/tokens');
}

export function createToken(name: string, expires_at?: string) {
  return request<ApiTokenCreated>('POST', '/api/v1/tokens', { name, expires_at });
}

export function revokeToken(tokenId: string) {
  return request<void>('DELETE', `/api/v1/tokens/${tokenId}`);
}

export function getPreferences() {
  return request<{ preferences: Record<string, unknown> }>('GET', '/api/v1/preferences');
}

export function savePreferences(prefs: Record<string, unknown>) {
  return request<{ preferences: Record<string, unknown> }>('PUT', '/api/v1/preferences', prefs);
}

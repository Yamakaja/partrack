import { request, qs } from './client.js';
import type { ParameterDefinition, DistributorParamMapping } from '$lib/types/parts.js';

export interface AdminUser {
  id: string;
  display_name: string;
  email: string;
  is_active: boolean;
  is_admin: boolean;
  has_password: boolean;
  created_at: string;
}

export function listUsers(limit = 50, offset = 0) {
  return request<{ items: AdminUser[]; total: number; limit: number; offset: number }>(
    'GET',
    `/api/v1/admin/users?limit=${limit}&offset=${offset}`,
  );
}

export function createUser(body: { display_name: string; email: string; password?: string }) {
  return request<AdminUser>('POST', '/api/v1/admin/users', body);
}

export function updateUser(
  userId: string,
  body: { display_name?: string; email?: string; is_active?: boolean; is_admin?: boolean },
) {
  return request<AdminUser>('PATCH', `/api/v1/admin/users/${userId}`, body);
}

export function setPassword(userId: string, password: string) {
  return request<void>('POST', `/api/v1/admin/users/${userId}/set-password`, { password });
}

export function generateResetLink(userId: string) {
  return request<{ path: string; expires_at: string }>(
    'POST',
    `/api/v1/admin/users/${userId}/password-reset-link`,
  );
}

export function getResetPasswordInfo(token: string) {
  return request<{ display_name: string; email: string; expires_at: string }>(
    'GET',
    `/api/v1/auth/reset-password?token=${encodeURIComponent(token)}`,
  );
}

export function completePasswordReset(token: string, password: string) {
  return request<{ user: import('$lib/types/auth.js').User; memberships: import('$lib/types/auth.js').TeamMembership[] }>(
    'POST',
    '/api/v1/auth/reset-password',
    { token, password },
  );
}

// ── Parameter registry ────────────────────────────────────────────────────────

export function listParameterDefinitions() {
  return request<ParameterDefinition[]>('GET', '/api/v1/admin/parameter-definitions');
}

export function createParameterDefinition(body: {
  slug: string;
  display_name: string;
  unit?: string | null;
  unit_symbol?: string | null;
  value_type: ParameterDefinition['value_type'];
}) {
  return request<ParameterDefinition>('POST', '/api/v1/admin/parameter-definitions', body);
}

export function updateParameterDefinition(
  id: number,
  body: Partial<Pick<ParameterDefinition, 'slug' | 'display_name' | 'unit' | 'unit_symbol' | 'value_type'>>,
) {
  return request<ParameterDefinition>('PATCH', `/api/v1/admin/parameter-definitions/${id}`, body);
}

export function deleteParameterDefinition(id: number) {
  return request<void>('DELETE', `/api/v1/admin/parameter-definitions/${id}`);
}

export function listDistributorParamMappings(params: { distributor_id?: string; unmapped?: boolean } = {}) {
  return request<DistributorParamMapping[]>(
    'GET',
    `/api/v1/admin/distributor-parameter-mappings?${qs(params)}`,
  );
}

export function updateDistributorParamMapping(
  id: string,
  body: { parameter_id?: number | null; ignored?: boolean },
) {
  return request<void>('PATCH', `/api/v1/admin/distributor-parameter-mappings/${id}`, body);
}

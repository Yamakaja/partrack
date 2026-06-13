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
  return request<ParameterDefinition & { reparsed_count: number }>(
    'PATCH',
    `/api/v1/admin/parameter-definitions/${id}`,
    body,
  );
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
  return request<{ reparsed_count: number }>(
    'PATCH',
    `/api/v1/admin/distributor-parameter-mappings/${id}`,
    body,
  );
}

export interface ParsePreviewResult {
  value: string;
  parsed: boolean;
  numeric_value: number | null;
  numeric_value_max: number | null;
}

/** Preview how a distributor mapping's real sample values parse under a value_type. */
export function parseMappingPreview(
  mappingId: string,
  valueType: ParameterDefinition['value_type'],
  limit = 12,
) {
  return request<{ value_type: string; results: ParsePreviewResult[] }>(
    'GET',
    `/api/v1/admin/distributor-parameter-mappings/${mappingId}/parse-preview?${qs({ value_type: valueType, limit })}`,
  );
}

// ── LDAP diagnostics ────────────────────────────────────────────────────────

export interface LdapProviderInfo {
  name: string;
  display_name: string;
  url: string;
}

export interface LdapStep {
  name: string;
  ok: boolean;
  detail?: string;
  err?: string;
}

export interface LdapMappingMatch {
  group: string;
  team: string;
  role: string;
  matched: boolean;
  valid_role: boolean;
}

export interface LdapDecision {
  required_group_configured: boolean;
  login_allowed: boolean;
  admin_group_configured: boolean;
  is_admin: boolean;
  team_roles: Record<string, string>;
  managed_teams: string[];
  mappings: LdapMappingMatch[];
}

export interface LdapTeamResolution {
  team: string;
  exists: boolean;
  intended_role?: string;
  action: string;
  current_role?: string;
  current_directory_managed?: boolean;
}

export interface LdapResolution {
  subject: string;
  outcome: string;
  linked_user_id?: string;
  linked_user_email?: string;
  existing_user_by_email_id?: string;
  existing_user_display_name?: string;
  auto_provision_enabled: boolean;
  teams: LdapTeamResolution[];
}

export interface LdapDiagnoseResult {
  provider: string;
  authenticated: boolean;
  error?: string;
  steps: LdapStep[];
  identity?: {
    subject: string;
    email: string;
    display_name: string;
    dn: string;
    groups: string[];
  };
  authorization?: LdapDecision;
  resolution?: LdapResolution;
  warnings?: string[];
}

export interface LdapDNCheck {
  dn: string;
  exists: boolean;
  err?: string;
}

export interface LdapConnectionResult {
  provider: string;
  connected: boolean;
  error?: string;
  steps: LdapStep[];
  dn_checks: LdapDNCheck[] | null;
  config: Record<string, unknown>;
}

export function testLdapConnection(provider: string) {
  return request<LdapConnectionResult>('POST', '/api/v1/admin/ldap/test-connection', { provider });
}

export function listLdapProviders() {
  return request<{ providers: LdapProviderInfo[] }>('GET', '/api/v1/admin/ldap/providers');
}

export function diagnoseLdap(provider: string, username: string, password: string) {
  return request<LdapDiagnoseResult>('POST', '/api/v1/admin/ldap/diagnose', {
    provider,
    username,
    password,
  });
}

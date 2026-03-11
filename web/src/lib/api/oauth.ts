import { request } from './client.js';

export function getDigiKeyAuthURL(teamId: string, returnTo: string) {
  const params = new URLSearchParams({ return_to: returnTo });
  return request<{ auth_url: string }>(
    'GET', `/api/v1/teams/${teamId}/oauth/digikey/initiate?${params}`,
  );
}

export interface OAuthStatus {
  connected: boolean;
  account_id: string;
  updated_at: string;
}

export function getOAuthStatus(teamId: string, distributorApiType: string) {
  return request<OAuthStatus>(
    'GET', `/api/v1/teams/${teamId}/oauth/${distributorApiType}/status`,
  );
}

export function disconnectOAuth(teamId: string, distributorApiType: string) {
  return request<void>(
    'DELETE', `/api/v1/teams/${teamId}/oauth/${distributorApiType}/token`,
  );
}

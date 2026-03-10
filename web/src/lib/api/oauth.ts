import { request } from './client.js';

export function getDigiKeyAuthURL(teamId: string, returnTo: string) {
  const params = new URLSearchParams({ return_to: returnTo });
  return request<{ auth_url: string }>(
    'GET', `/api/v1/teams/${teamId}/oauth/digikey/initiate?${params}`,
  );
}

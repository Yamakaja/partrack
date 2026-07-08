import { request } from './client.js';

export interface SourcingPolicyDistributor {
  distributor_id: string;
  distributor_name: string;
  enabled: boolean;
  priority: number | null;
}

export interface SourcingPolicy {
  strategy: 'cost' | 'preferred_in_stock';
  cost_tolerance_pct: string | null;
  cost_tolerance_abs: string | null;
  distributors: SourcingPolicyDistributor[];
}

export interface UpdateSourcingPolicyBody {
  strategy: 'cost' | 'preferred_in_stock';
  cost_tolerance_pct?: number | null;
  cost_tolerance_abs?: number | null;
  distributor_priority: string[];
}

export function getSourcingPolicy(teamId: string) {
  return request<SourcingPolicy>('GET', `/api/v1/teams/${teamId}/sourcing-policy`);
}

export function updateSourcingPolicy(teamId: string, body: UpdateSourcingPolicyBody) {
  return request<SourcingPolicy>('PUT', `/api/v1/teams/${teamId}/sourcing-policy`, body);
}

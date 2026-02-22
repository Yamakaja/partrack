import { request } from './client.js';
import type { Project, ProjectSummary } from '$lib/types/projects.js';
import type { PaginatedResponse } from '$lib/types/common.js';

export function listProjects(teamId: string) {
  return request<PaginatedResponse<ProjectSummary>>('GET', `/api/v1/teams/${teamId}/projects`);
}

export function createProject(teamId: string, name: string, description?: string) {
  return request<Project>('POST', `/api/v1/teams/${teamId}/projects`, { name, description });
}

export function getProject(teamId: string, projectId: string) {
  return request<Project>('GET', `/api/v1/teams/${teamId}/projects/${projectId}`);
}

export function updateProject(teamId: string, projectId: string, body: { name?: string; description?: string }) {
  return request<Project>('PATCH', `/api/v1/teams/${teamId}/projects/${projectId}`, body);
}

export function setActiveBOM(teamId: string, projectId: string, revisionId: string) {
  return request<Project>('POST', `/api/v1/teams/${teamId}/projects/${projectId}/set-active-bom`, { revision_id: revisionId });
}

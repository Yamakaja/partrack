import { request } from './client.js';

export interface NoteManifestAttachment {
  url: string;
  content_type: string;
  width?: number;
  height?: number;
}

export interface NoteManifestObject {
  kind: string;
  label: string;
  href: string;
  status?: string;
}

export interface NoteManifest {
  attachments: Record<string, NoteManifestAttachment>;
  objects: Record<string, NoteManifestObject>;
}

export type PartFlag = 'none' | 'preferred' | 'approved' | 'banned';

export interface Note {
  body: string;
  manifest: NoteManifest;
  flag?: PartFlag; // part notes only
  updated_at?: string;
  updated_by?: string;
}

export function getPartNote(teamId: string, partId: string) {
  return request<Note>('GET', `/api/v1/teams/${teamId}/parts/${partId}/note`);
}

export function putPartNote(teamId: string, partId: string, body: string) {
  return request<Note>('PUT', `/api/v1/teams/${teamId}/parts/${partId}/note`, { body });
}

export function setPartFlag(teamId: string, partId: string, flag: PartFlag) {
  return request<{ flag: PartFlag }>('PUT', `/api/v1/teams/${teamId}/parts/${partId}/flag`, { flag });
}

export function getProjectNote(teamId: string, projectId: string) {
  return request<Note>('GET', `/api/v1/teams/${teamId}/projects/${projectId}/note`);
}

export function putProjectNote(teamId: string, projectId: string, body: string) {
  return request<Note>('PUT', `/api/v1/teams/${teamId}/projects/${projectId}/note`, { body });
}

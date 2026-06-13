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

export interface PartNote {
  body: string;
  manifest: NoteManifest;
  updated_at?: string;
  updated_by?: string;
}

export function getPartNote(teamId: string, partId: string) {
  return request<PartNote>('GET', `/api/v1/teams/${teamId}/parts/${partId}/note`);
}

export function putPartNote(teamId: string, partId: string, body: string) {
  return request<PartNote>('PUT', `/api/v1/teams/${teamId}/parts/${partId}/note`, { body });
}

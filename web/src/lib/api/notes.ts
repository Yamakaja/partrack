import { request } from './client.js';

export interface NoteManifestAttachment {
  url: string;
  content_type: string;
  width?: number;
  height?: number;
}

export interface NoteManifestPart {
  mpn: string | null;
  manufacturer: string | null;
  lifecycle_status: string;
}

export interface NoteManifest {
  attachments: Record<string, NoteManifestAttachment>;
  parts: Record<string, NoteManifestPart>;
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

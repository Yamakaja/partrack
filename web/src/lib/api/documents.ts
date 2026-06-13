import { request } from './client.js';

// A document is a file attachment (datasheet, office file, image, Touchstone
// S-params, …) pinned to a part or project — listed separately from note
// markdown. `viewable` means it can be previewed in-app (PDF or raster image).
export interface DocumentItem {
  id: string;
  filename: string;
  content_type: string;
  byte_size: number;
  viewable: boolean;
  uploaded_by?: string;
  uploaded_at?: string;
}

export type DocumentOwnerKind = 'part' | 'project';

function ownerPath(kind: DocumentOwnerKind): string {
  return kind === 'part' ? 'parts' : 'projects';
}

export async function listDocuments(
  teamId: string,
  ownerKind: DocumentOwnerKind,
  ownerId: string,
): Promise<DocumentItem[]> {
  const res = await request<{ documents: DocumentItem[] }>(
    'GET',
    `/api/v1/teams/${teamId}/${ownerPath(ownerKind)}/${ownerId}/documents`,
  );
  return res.documents ?? [];
}

// Document uploads are raw bytes (like image attachments), so they bypass the
// JSON `request` helper. The server classifies by the ?filename extension and
// sniffs inline-served types for safety.
export async function uploadDocument(
  teamId: string,
  ownerKind: DocumentOwnerKind,
  ownerId: string,
  file: File,
): Promise<DocumentItem> {
  const res = await fetch(
    `/api/v1/teams/${teamId}/${ownerPath(ownerKind)}/${ownerId}/documents?filename=${encodeURIComponent(file.name)}`,
    {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
      body: file,
    },
  );
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error ?? `upload failed: HTTP ${res.status}`);
  }
  return res.json();
}

export function deleteDocument(teamId: string, docId: string): Promise<void> {
  return request<void>('DELETE', `/api/v1/teams/${teamId}/documents/${docId}`);
}

// documentContentUrl is the access-controlled, filename-aware serve path. PDFs
// and images render inline (for the viewer); pass { download: true } to force a
// download with the original filename.
export function documentContentUrl(
  teamId: string,
  docId: string,
  opts?: { download?: boolean },
): string {
  const base = `/api/v1/teams/${teamId}/documents/${docId}/content`;
  return opts?.download ? `${base}?download=1` : base;
}

// formatBytes renders a human-readable size (1024-based).
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}

// Attachment uploads are raw image bytes, not JSON, so they bypass the shared
// `request` helper. The server sniffs+validates the content type from the bytes
// (the Content-Type header is advisory) and returns the blob URL to render.
export interface UploadedAttachment {
  id: string;
  url: string;
  content_type: string;
  byte_size: number;
  width?: number;
  height?: number;
  filename?: string;
}

export async function uploadAttachment(teamId: string, file: File): Promise<UploadedAttachment> {
  const res = await fetch(
    `/api/v1/teams/${teamId}/attachments?filename=${encodeURIComponent(file.name)}`,
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

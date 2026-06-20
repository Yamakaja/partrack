import { request } from './client.js';

export interface ChangelogRelease {
  version: string;
  date: string;
  entries: string[];
}

/** Releases newer than `since` (omit `since` for all releases). */
export function getChangelog(since?: string) {
  const q = since ? `?since=${encodeURIComponent(since)}` : '';
  return request<{ releases: ChangelogRelease[] }>('GET', `/api/v1/changelog${q}`);
}

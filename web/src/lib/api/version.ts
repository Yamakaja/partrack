import { request } from './client.js';

export interface VersionInfo {
  version: string;
  commit: string;
  date: string;
}

export function getVersion() {
  return request<VersionInfo>('GET', '/api/v1/version');
}

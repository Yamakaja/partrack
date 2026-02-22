import { savePreferences } from '$lib/api/auth.js';

export const preferences = $state<Record<string, unknown>>({});

export function setPreferences(prefs: Record<string, unknown>) {
  Object.assign(preferences, prefs);
}

export async function setPref(key: string, value: unknown) {
  preferences[key] = value;
  await savePreferences({ ...preferences });
}

export function getPref<T>(key: string, fallback: T): T {
  return (key in preferences ? preferences[key] : fallback) as T;
}

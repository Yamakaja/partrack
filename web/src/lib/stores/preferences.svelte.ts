import { savePreferences, getPreferences } from '$lib/api/auth.js';

export const preferences = $state<Record<string, unknown>>({});

export function setPreferences(prefs: Record<string, unknown>) {
  Object.assign(preferences, prefs);
}

/**
 * Load the signed-in user's saved preferences into the store. Must run before
 * any setPref() call, otherwise setPref would PUT a blob missing the existing
 * server-side keys (the API replaces the whole object).
 */
export async function initPreferences() {
  try {
    const { preferences: prefs } = await getPreferences();
    setPreferences(prefs);
  } catch {
    // Non-fatal: fall back to an empty preference set.
  }
}

export async function setPref(key: string, value: unknown) {
  preferences[key] = value;
  await savePreferences({ ...preferences });
}

export function getPref<T>(key: string, fallback: T): T {
  return (key in preferences ? preferences[key] : fallback) as T;
}

import { getVersion, type VersionInfo } from '$lib/api/version.js';

export const versionStore = $state<{ info: VersionInfo | null }>({ info: null });

/** Fetch the running build's version once at startup. Cosmetic — failures are ignored. */
export async function initVersion() {
  try {
    versionStore.info = await getVersion();
  } catch {
    // Non-fatal: version display is purely informational.
  }
}

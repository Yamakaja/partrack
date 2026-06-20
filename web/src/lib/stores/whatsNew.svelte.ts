import { versionStore } from './version.svelte.js';
import { getVersion } from '$lib/api/version.js';
import { getPref, setPref } from './preferences.svelte.js';
import { getChangelog, type ChangelogRelease } from '$lib/api/changelog.js';

export const whatsNewStore = $state<{ open: boolean; releases: ChangelogRelease[] }>({
  open: false,
  releases: [],
});

const RELEASE_RE = /^\d+\.\d+\.\d+$/;

function parse(v: string): [number, number, number] | null {
  if (!RELEASE_RE.test(v)) return null;
  const [a, b, c] = v.split('.').map(Number);
  return [a, b, c];
}

/** -1 if a<b, 0 if equal, 1 if a>b. Both must be clean releases. */
function cmp(a: [number, number, number], b: [number, number, number]): number {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1;
  }
  return 0;
}

/**
 * Show the "what's changed" dialog when the running build is a newer tagged
 * release than the one this user last saw. Gated entirely on the user's stored
 * `lastSeenVersion` preference (no schema). Silent for first-time users,
 * downgrades, and dev/untagged builds.
 */
export async function checkWhatsNew() {
  const info = versionStore.info ?? (await getVersion().catch(() => null));
  const current = info?.version;
  const cur = current ? parse(current) : null;
  if (!current || !cur) return; // dev / untagged build → nothing to show

  const lastSeen = getPref<string | null>('lastSeenVersion', null);
  const prev = lastSeen ? parse(lastSeen) : null;

  // First time we record a version, or a downgrade/rollback: just remember the
  // current version, don't surface a backlog dialog.
  if (!prev || cmp(prev, cur) >= 0) {
    if (lastSeen !== current) await setPref('lastSeenVersion', current);
    return;
  }

  try {
    const { releases } = await getChangelog(lastSeen ?? undefined);
    if (releases && releases.length > 0) {
      whatsNewStore.releases = releases;
      whatsNewStore.open = true;
    }
    await setPref('lastSeenVersion', current); // mark seen on success
  } catch {
    // Leave lastSeenVersion unchanged so it retries on the next load.
  }
}

export function dismissWhatsNew() {
  whatsNewStore.open = false;
}

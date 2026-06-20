import { currentRoute } from '$lib/router/index.svelte.js';
import { chapterForPath, DEFAULT_CHAPTER } from '$lib/help/chapters.js';

export const helpStore = $state<{ open: boolean; slug: string }>({
  open: false,
  slug: DEFAULT_CHAPTER,
});

/** Open the help drawer to a specific chapter (defaults to the current one). */
export function openHelp(slug?: string) {
  if (slug) helpStore.slug = slug;
  helpStore.open = true;
}

/** Open the help drawer to the chapter documenting the current page. */
export function openHelpForCurrentRoute() {
  helpStore.slug = chapterForPath(currentRoute().path);
  helpStore.open = true;
}

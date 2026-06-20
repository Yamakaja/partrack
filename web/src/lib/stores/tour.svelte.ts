import { getPref, setPref } from './preferences.svelte.js';
import { whatsNewStore } from './whatsNew.svelte.js';
import { activeTeamStore } from './activeTeam.svelte.js';
import { openHelp } from './help.svelte.js';

// Versioned so a future feature-tour can be introduced without re-showing this one.
const TOUR_KEY = 'tour.v1';

export interface TourStep {
  /** CSS selector for the element to spotlight; omitted → a centered card. */
  target?: string;
  /** Preferred side of the target to place the card. */
  side?: 'top' | 'right' | 'bottom' | 'left';
  title: string;
  body: string;
}

export const STEPS: TourStep[] = [
  {
    title: 'Welcome to partrack 👋',
    body: "partrack tracks your parts, turns BOMs into orders, and keeps stock accurate as you build. Here's a one-minute look at the key controls.",
  },
  {
    target: '[data-tour="nav"]',
    side: 'right',
    title: 'Your workspace',
    body: "Everything lives in this sidebar — Parts, Storage, Projects, Cart, Orders and Builds. The switcher at the top changes which team you're in; everything is scoped to a team.",
  },
  {
    target: '[data-tour="search"]',
    side: 'bottom',
    title: 'Find anything',
    body: 'Jump straight to any part, project, or order from any page. Press ⌘K (Ctrl-K) to open it instantly.',
  },
  {
    target: '[data-tour="help"]',
    side: 'bottom',
    title: 'Help, in context',
    body: "Stuck on a page? This opens the user guide to that page's chapter — your map whenever something's unclear.",
  },
  {
    target: '[data-tour="status"]',
    side: 'right',
    title: 'Live status',
    body: 'The top dot is your live link to the server — updates appear without refreshing. Below it, Connect Phone pairs your phone as a barcode scanner.',
  },
  {
    target: '[data-tour="account"]',
    side: 'right',
    title: 'Your account',
    body: 'Your profile, sessions and API tokens live here, along with the light/dark toggle, sign-out, and the running version.',
  },
  {
    title: "You're set",
    body: "That's the tour. When you're ready to run your first build, the Getting started guide walks you through it end to end.",
  },
];

export const tourStore = $state<{ active: boolean; index: number }>({
  active: false,
  index: 0,
});

export function startTour() {
  tourStore.index = 0;
  tourStore.active = true;
}

export function nextStep() {
  if (tourStore.index >= STEPS.length - 1) {
    finishTour();
  } else {
    tourStore.index++;
  }
}

export function prevStep() {
  if (tourStore.index > 0) tourStore.index--;
}

export function finishTour() {
  tourStore.active = false;
  void setPref(TOUR_KEY, true); // remember; shown once, ever
}

export const skipTour = finishTour;

/** Last step's CTA: open the Getting started guide, then close the tour. */
export function finishAndOpenGuide() {
  openHelp('getting-started');
  finishTour();
}

/**
 * Start the first-run tour when appropriate: the user hasn't seen it, a team is
 * active (so the sidebar/top bar exist to point at), and the "what's changed"
 * dialog isn't claiming this session. Safe to call repeatedly.
 */
export function maybeStartTour() {
  if (tourStore.active) return;
  if (getPref(TOUR_KEY, false)) return;
  if (whatsNewStore.shownThisSession) return;
  if (!activeTeamStore.current) return;
  startTour();
}

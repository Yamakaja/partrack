import { matchRoute } from '$lib/router/index.svelte.js';

// Eagerly bundle every user-guide chapter as a raw markdown string. The corpus
// lives at web/src/userguide/ and is also mdBook-compatible (see SUMMARY.md) so
// the same files can build an external doc site later.
const files = import.meta.glob('../../userguide/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const bySlug: Record<string, string> = {};
for (const [path, content] of Object.entries(files)) {
  const slug = path.split('/').pop()!.replace(/\.md$/, '');
  bySlug[slug] = content;
}

export interface Chapter {
  /** Filename (without .md) and stable id used by the drawer + page mapping. */
  slug: string;
  title: string;
  /** Route patterns (router syntax) this chapter documents. */
  routes: string[];
}

// Order here is the order shown in the help drawer's table of contents and must
// mirror userguide/SUMMARY.md.
export const CHAPTERS: Chapter[] = [
  {
    slug: 'getting-started',
    title: 'Getting started',
    routes: ['/', '/teams/:teamId', '/settings/profile', '/settings/sessions', '/settings/tokens'],
  },
  {
    slug: 'first-project',
    title: 'Your first project',
    routes: [],
  },
  {
    slug: 'parts-inventory',
    title: 'Parts & inventory',
    routes: [
      '/teams/:teamId/parts',
      '/teams/:teamId/parts/:partId',
      '/teams/:teamId/checkouts',
    ],
  },
  {
    slug: 'storage',
    title: 'Storage',
    routes: ['/teams/:teamId/storage', '/teams/:teamId/storage/:locationId'],
  },
  {
    slug: 'projects-bom',
    title: 'Projects & BOMs',
    routes: [
      '/teams/:teamId/projects',
      '/teams/:teamId/projects/:projectId',
      '/teams/:teamId/projects/:projectId/revisions/:revId',
      '/teams/:teamId/projects/:projectId/interpretation-memory',
    ],
  },
  {
    slug: 'cart',
    title: 'Shopping cart',
    routes: ['/teams/:teamId/cart'],
  },
  {
    slug: 'lists',
    title: 'Lists',
    routes: ['/teams/:teamId/lists'],
  },
  {
    slug: 'orders',
    title: 'Orders',
    routes: ['/teams/:teamId/orders', '/teams/:teamId/orders/:orderId'],
  },
  {
    slug: 'builds',
    title: 'Builds',
    routes: ['/teams/:teamId/builds', '/teams/:teamId/builds/:buildId'],
  },
  {
    slug: 'scanning',
    title: 'Scanning',
    routes: ['/teams/:teamId/bulk-scan', '/teams/:teamId/bulk-scan/:sessionId', '/scan'],
  },
  {
    slug: 'roles-and-permissions',
    title: 'Roles & permissions',
    routes: ['/teams/:teamId/settings', '/teams/:teamId/settings/members'],
  },
  {
    slug: 'administration',
    title: 'Administration',
    routes: [
      '/admin',
      '/admin/users',
      '/admin/parameters',
      '/admin/category-parameters',
      '/admin/ldap',
      '/teams/:teamId/settings/distributors',
    ],
  },
];

export const DEFAULT_CHAPTER = 'getting-started';

/** Raw markdown for a chapter, or empty string if the slug is unknown. */
export function chapterContent(slug: string): string {
  return bySlug[slug] ?? '';
}

export function chapterTitle(slug: string): string {
  return CHAPTERS.find((c) => c.slug === slug)?.title ?? 'Help';
}

/** Resolve the most relevant chapter for a route path; falls back to overview. */
export function chapterForPath(path: string): string {
  for (const ch of CHAPTERS) {
    for (const pat of ch.routes) {
      if (matchRoute(pat, path)) return ch.slug;
    }
  }
  return DEFAULT_CHAPTER;
}

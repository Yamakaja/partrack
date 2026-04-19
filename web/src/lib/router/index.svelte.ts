// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface RouteParams {
  [key: string]: string;
}

export interface Route {
  path: string;
  hash: string;
  params: RouteParams;
  /** Increments on every push navigation and popstate; use to detect same-path hash changes. */
  id: number;
}

// ---------------------------------------------------------------------------
// Reactive current route (Svelte 5 runes-compatible module state)
// ---------------------------------------------------------------------------

let _route = $state<Route>({ path: window.location.pathname, hash: window.location.hash, params: {}, id: 0 });

export function currentRoute(): Route {
  return _route;
}

// ---------------------------------------------------------------------------
// Pattern matching
// ---------------------------------------------------------------------------

/**
 * Converts a route pattern like "/teams/:teamId/inventory" into a RegExp
 * and a list of param names.
 */
function compilePattern(pattern: string): { re: RegExp; keys: string[] } {
  const keys: string[] = [];
  const reStr = pattern
    .replace(/\//g, '\\/')
    .replace(/:([a-zA-Z_][a-zA-Z0-9_]*)/g, (_m, key) => {
      keys.push(key);
      return '([^\\/]+)';
    });
  return { re: new RegExp(`^${reStr}$`), keys };
}

export function matchRoute(pattern: string, path: string): RouteParams | null {
  const { re, keys } = compilePattern(pattern);
  const m = path.match(re);
  if (!m) return null;
  const params: RouteParams = {};
  keys.forEach((k, i) => { params[k] = m[i + 1]; });
  return params;
}

// ---------------------------------------------------------------------------
// Navigation
// ---------------------------------------------------------------------------

export function navigate(path: string, replace = false) {
  if (replace) {
    history.replaceState(null, '', path);
  } else {
    history.pushState(null, '', path);
  }
  // params resolved by Router component; id increments only on push so replace-state
  // URL syncs (e.g. filter writes) don't re-trigger hash-watching effects.
  _route = { path: window.location.pathname, hash: window.location.hash, params: {}, id: replace ? _route.id : _route.id + 1 };
}

window.addEventListener('popstate', () => {
  _route = { path: window.location.pathname, hash: window.location.hash, params: {}, id: _route.id + 1 };
});

// ---------------------------------------------------------------------------
// Link component helper — used via <a> with onclick
// ---------------------------------------------------------------------------

export function linkAction(node: HTMLAnchorElement) {
  function handleClick(e: MouseEvent) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const href = node.getAttribute('href');
    if (!href || href.startsWith('http') || href.startsWith('//')) return;
    e.preventDefault();
    navigate(href);
  }
  node.addEventListener('click', handleClick);
  return {
    destroy() { node.removeEventListener('click', handleClick); },
  };
}
